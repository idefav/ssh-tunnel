package tunnel

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestTrafficStore(t *testing.T, location *time.Location) (*TrafficStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, location)
	if err != nil {
		t.Fatalf("open traffic store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestMeteredConnCountsReadWriteBeforeClose(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.Local)
	left, right := net.Pipe()
	metered := &meteredConn{Conn: left, store: store, profileID: "jp"}
	defer metered.Close()
	defer right.Close()

	upload := []byte("upload bytes")
	uploadDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, len(upload))
		_, err := io.ReadFull(right, buffer)
		uploadDone <- err
	}()
	if _, err := metered.Write(upload); err != nil {
		t.Fatalf("metered write: %v", err)
	}
	if err := <-uploadDone; err != nil {
		t.Fatalf("peer read: %v", err)
	}

	download := []byte("download bytes")
	downloadDone := make(chan error, 1)
	go func() {
		_, err := right.Write(download)
		downloadDone <- err
	}()
	buffer := make([]byte, len(download))
	if _, err := io.ReadFull(metered, buffer); err != nil {
		t.Fatalf("metered read: %v", err)
	}
	if err := <-downloadDone; err != nil {
		t.Fatalf("peer write: %v", err)
	}

	summary := store.Summary()
	if summary.Overall.UploadBytesTotal != uint64(len(upload)) || summary.Overall.DownloadBytesTotal != uint64(len(download)) {
		t.Fatalf("unexpected overall totals: %+v", summary.Overall)
	}
	if len(summary.Profiles) != 1 || summary.Profiles[0].ProfileID != "jp" || summary.Profiles[0].UploadBytesTotal != uint64(len(upload)) {
		t.Fatalf("unexpected profile totals: %+v", summary.Profiles)
	}
}

func TestTrafficStoreFlushCloseAndRestartRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	store.Record("jp", 10, 20)
	store.Record("", 3, 4)
	if err := store.Close(); err != nil {
		t.Fatalf("close with final flush: %v", err)
	}

	reopened, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	summary := reopened.Summary()
	if summary.Overall.UploadBytesTotal != 13 || summary.Overall.DownloadBytesTotal != 24 {
		t.Fatalf("overall not restored: %+v", summary.Overall)
	}
	if summary.Direct.UploadBytesTotal != 3 || summary.Direct.DownloadBytesTotal != 4 {
		t.Fatalf("direct not restored: %+v", summary.Direct)
	}
}

func TestTrafficStoreUsesPrivateFileAndRejectsLockConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("traffic database permissions = %v", info.Mode().Perm())
	}
	if second, err := openTrafficStoreAt(path, time.Local); err == nil {
		_ = second.Close()
		t.Fatal("expected a second writer to fail on the database lock")
	}
}

func TestTrafficStorePeriodicFlushLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store.recordAt("jp", now, 7, 11)
	pending := store.pendingCounter(trafficHourKey{Scope: trafficScopeAll, Hour: localHourStart(now, time.Local).Unix()})
	if pending.upload.Load() == 0 {
		t.Fatal("expected traffic to be pending before periodic flush")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runTrafficStoreLoop(ctx, store, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for pending.upload.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pending.upload.Load() != 0 {
		cancel()
		<-done
		t.Fatal("periodic flush did not persist pending traffic")
	}
	cancel()
	<-done
	reopened, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if summary := reopened.Summary(); summary.Overall.UploadBytesTotal != 7 || summary.Overall.DownloadBytesTotal != 11 {
		t.Fatalf("periodic flush was not durable: %+v", summary.Overall)
	}
}

func TestTrafficStoreConcurrentAtomicAccountingAndFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	const iterations = 500
	var records sync.WaitGroup
	flushStopped := make(chan struct{})
	flushDone := make(chan struct{})
	flushErrors := make(chan error, 1)
	go func() {
		defer close(flushDone)
		for {
			select {
			case <-flushStopped:
				return
			default:
				if err := store.Flush(); err != nil {
					flushErrors <- err
					return
				}
			}
		}
	}()
	for worker := 0; worker < workers; worker++ {
		records.Add(1)
		go func(worker int) {
			defer records.Done()
			profileID := ""
			if worker%3 == 1 {
				profileID = "jp"
			} else if worker%3 == 2 {
				profileID = "us"
			}
			for i := 0; i < iterations; i++ {
				store.Record(profileID, 1, 2)
			}
		}(worker)
	}
	records.Wait()
	close(flushStopped)
	<-flushDone
	select {
	case err := <-flushErrors:
		t.Fatal(err)
	default:
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openTrafficStoreAt(path, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summary := reopened.Summary()
	expected := uint64(workers * iterations)
	if summary.Overall.UploadBytesTotal != expected || summary.Overall.DownloadBytesTotal != expected*2 {
		t.Fatalf("concurrent totals lost: %+v", summary.Overall)
	}
	var upload, download uint64
	for _, profile := range summary.Profiles {
		upload += profile.UploadBytesTotal
		download += profile.DownloadBytesTotal
	}
	upload += summary.Direct.UploadBytesTotal
	download += summary.Direct.DownloadBytesTotal
	if upload != summary.Overall.UploadBytesTotal || download != summary.Overall.DownloadBytesTotal {
		t.Fatalf("detail sum mismatch: %+v", summary)
	}
}

func TestTrafficStoreResetKeepsDetailSumAndHistory(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.Local)
	store.Record("jp", 50, 70)
	store.Record("us", 20, 30)
	store.Record("", 5, 7)
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Hour)
	after := time.Now().Add(time.Hour)
	historyBefore, err := store.History(trafficScopeAll, before, after, "hour")
	if err != nil || len(historyBefore.Points) == 0 {
		t.Fatalf("expected history before reset: %+v err=%v", historyBefore, err)
	}
	if err := store.ResetProfile("jp"); err != nil {
		t.Fatal(err)
	}
	summary := store.Summary()
	if summary.Overall.UploadBytesTotal != 25 || summary.Overall.DownloadBytesTotal != 37 {
		t.Fatalf("overall did not subtract profile reset: %+v", summary.Overall)
	}
	var detailUpload, detailDownload uint64
	for _, item := range summary.Profiles {
		detailUpload += item.UploadBytesTotal
		detailDownload += item.DownloadBytesTotal
	}
	detailUpload += summary.Direct.UploadBytesTotal
	detailDownload += summary.Direct.DownloadBytesTotal
	if detailUpload != summary.Overall.UploadBytesTotal || detailDownload != summary.Overall.DownloadBytesTotal {
		t.Fatalf("overall/detail invariant broken: summary=%+v", summary)
	}
	historyAfter, err := store.History(trafficScopeAll, before, after, "hour")
	if err != nil || len(historyAfter.Points) != len(historyBefore.Points) || historyAfter.Points[0] != historyBefore.Points[0] {
		t.Fatalf("profile reset changed history: before=%+v after=%+v err=%v", historyBefore, historyAfter, err)
	}

	if err := store.ResetAll(); err != nil {
		t.Fatal(err)
	}
	summary = store.Summary()
	if summary.Overall.UploadBytesTotal != 0 || summary.Direct.DownloadBytesTotal != 0 || summary.Overall.LastResetAt.IsZero() {
		t.Fatalf("all reset failed: %+v", summary)
	}
}

func TestTrafficStoreResetAllRecordsEveryKnownProfile(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.Local)
	store.EnsureProfile("unused")
	if err := store.ResetAll(); err != nil {
		t.Fatal(err)
	}
	summary := store.Summary()
	if len(summary.Profiles) != 1 || summary.Profiles[0].ProfileID != "unused" || summary.Profiles[0].LastResetAt.IsZero() {
		t.Fatalf("known zero-traffic profile did not receive reset timestamp: %+v", summary.Profiles)
	}
}

func TestTrafficHistoryUsesLocalHourAndAggregates(t *testing.T) {
	location := time.FixedZone("test-zone", 8*60*60)
	store, _ := newTestTrafficStore(t, location)
	first := time.Date(2026, 8, 27, 10, 15, 0, 0, location)
	second := first.Add(time.Hour)
	store.recordAt("", first, 10, 20)
	store.recordAt("", second, 30, 40)
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	history, err := store.History(trafficScopeAll, first.Add(-time.Hour), second.Add(2*time.Hour), "day")
	if err != nil {
		t.Fatal(err)
	}
	if history.Timezone != "test-zone" || len(history.Points) != 1 || history.Points[0].UploadBytes != 40 || history.Points[0].Start.Hour() != 0 {
		t.Fatalf("unexpected local aggregation: %+v", history)
	}
	monthly, err := store.History(trafficScopeAll, first.Add(-time.Hour), second.Add(2*time.Hour), "month")
	if err != nil || len(monthly.Points) != 1 || monthly.Points[0].Start.Day() != 1 || monthly.Points[0].UploadBytes != 40 {
		t.Fatalf("unexpected monthly aggregation: %+v err=%v", monthly, err)
	}
}

func TestTrafficHistoryRetentionCleanup(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.Local)
	now := time.Now()
	store.recordAt("", now.Add(-366*24*time.Hour), 1, 1)
	store.recordAt("", now.Add(-24*time.Hour), 2, 2)
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	store.flushMu.Lock()
	store.lastClean = time.Time{}
	if err := store.cleanupLocked(now); err != nil {
		store.flushMu.Unlock()
		t.Fatal(err)
	}
	store.flushMu.Unlock()
	history, err := store.History(trafficScopeAll, now.Add(-400*24*time.Hour), now.Add(time.Hour), "month")
	if err != nil {
		t.Fatal(err)
	}
	var upload uint64
	for _, point := range history.Points {
		upload += point.UploadBytes
	}
	if upload != 2 {
		t.Fatalf("expired history was not removed: %+v", history.Points)
	}
}
