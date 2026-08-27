package tunnel

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"ssh-tunnel/cfg"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	trafficScopeAll      = "all"
	trafficScopeDirect   = "direct"
	trafficProfilePrefix = "profile:"
	trafficRetention     = 365 * 24 * time.Hour
)

var (
	trafficTotalsBucket = []byte("traffic_totals_v1")
	trafficHourlyBucket = []byte("traffic_hourly_v1")
)

type trafficValue struct {
	UploadBytes   uint64    `json:"uploadBytes"`
	DownloadBytes uint64    `json:"downloadBytes"`
	ResetAt       time.Time `json:"resetAt,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
}

type trafficHourKey struct {
	Scope string
	Hour  int64
}

type trafficSpeedSample struct {
	At       time.Time
	Upload   uint64
	Download uint64
}

type trafficCounter struct {
	upload    atomic.Uint64
	download  atomic.Uint64
	resetAt   atomic.Int64
	updatedAt atomic.Int64
}

func newTrafficCounter(value trafficValue) *trafficCounter {
	counter := &trafficCounter{}
	counter.upload.Store(value.UploadBytes)
	counter.download.Store(value.DownloadBytes)
	counter.resetAt.Store(unixNanoOrZero(value.ResetAt))
	counter.updatedAt.Store(unixNanoOrZero(value.UpdatedAt))
	return counter
}

func unixNanoOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

func (c *trafficCounter) snapshot() trafficValue {
	return trafficValue{
		UploadBytes:   c.upload.Load(),
		DownloadBytes: c.download.Load(),
		ResetAt:       timeFromUnixNano(c.resetAt.Load()),
		UpdatedAt:     timeFromUnixNano(c.updatedAt.Load()),
	}
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}

func storeLatestUnixNano(target *atomic.Int64, value int64) {
	for current := target.Load(); value > current; current = target.Load() {
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

type TrafficScopeMetrics struct {
	Scope              string    `json:"scope"`
	ProfileID          string    `json:"profileId,omitempty"`
	UploadBytesTotal   uint64    `json:"uploadBytesTotal"`
	DownloadBytesTotal uint64    `json:"downloadBytesTotal"`
	UploadBps          float64   `json:"uploadBps"`
	DownloadBps        float64   `json:"downloadBps"`
	LastResetAt        time.Time `json:"lastResetAt,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

type TrafficSummary struct {
	Overall   TrafficScopeMetrics   `json:"overall"`
	Profiles  []TrafficScopeMetrics `json:"profiles"`
	Direct    TrafficScopeMetrics   `json:"direct"`
	UpdatedAt time.Time             `json:"updatedAt,omitempty"`
}

type TrafficHistoryPoint struct {
	Start         time.Time `json:"start"`
	UploadBytes   uint64    `json:"uploadBytes"`
	DownloadBytes uint64    `json:"downloadBytes"`
}

type TrafficHistory struct {
	Scope    string                `json:"scope"`
	GroupBy  string                `json:"groupBy"`
	Timezone string                `json:"timezone"`
	From     time.Time             `json:"from"`
	To       time.Time             `json:"to"`
	Points   []TrafficHistoryPoint `json:"points"`
}

type TrafficStore struct {
	db          *bolt.DB
	location    *time.Location
	lifecycleMu sync.RWMutex
	flushMu     sync.Mutex
	totals      sync.Map // map[string]*trafficCounter
	pending     sync.Map // map[trafficHourKey]*trafficCounter
	samplesMu   sync.Mutex
	samples     map[string]trafficSpeedSample
	closed      atomic.Bool
	lastClean   time.Time
}

func OpenTrafficStore() (*TrafficStore, error) {
	dbPath, err := cfg.ResolveStateFilePath("traffic.db")
	if err != nil {
		return nil, err
	}
	return openTrafficStoreAt(dbPath, time.Local)
}

func openTrafficStoreAt(dbPath string, location *time.Location) (*TrafficStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("创建流量数据库目录失败: %w", err)
	}
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开流量数据库失败(%s): %w", dbPath, err)
	}
	if err := os.Chmod(dbPath, 0600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("设置流量数据库权限失败(%s): %w", dbPath, err)
	}
	store := &TrafficStore{
		db:       db,
		location: location,
		samples:  make(map[string]trafficSpeedSample),
	}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *TrafficStore) initialize() error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(trafficTotalsBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(trafficHourlyBucket)
		return err
	}); err != nil {
		return fmt.Errorf("初始化流量数据库失败: %w", err)
	}
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(trafficTotalsBucket)
		return bucket.ForEach(func(key, value []byte) error {
			var item trafficValue
			if err := json.Unmarshal(value, &item); err != nil {
				return fmt.Errorf("解析流量累计%s失败: %w", string(key), err)
			}
			s.totals.Store(string(key), newTrafficCounter(item))
			return nil
		})
	}); err != nil {
		return err
	}
	s.counterForScope(trafficScopeAll)
	s.counterForScope(trafficScopeDirect)
	return s.cleanupLocked(time.Now())
}

func trafficScopeForProfile(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return trafficScopeDirect
	}
	return trafficProfilePrefix + profileID
}

func profileIDFromTrafficScope(scope string) string {
	return strings.TrimPrefix(scope, trafficProfilePrefix)
}

func (s *TrafficStore) Record(profileID string, uploadBytes, downloadBytes uint64) {
	if s == nil || (uploadBytes == 0 && downloadBytes == 0) {
		return
	}
	s.recordAt(profileID, time.Now(), uploadBytes, downloadBytes)
}

func (s *TrafficStore) EnsureProfile(profileID string) {
	if s == nil || strings.TrimSpace(profileID) == "" {
		return
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if !s.closed.Load() {
		s.counterForScope(trafficScopeForProfile(profileID))
	}
}

func (s *TrafficStore) HasProfile(profileID string) bool {
	if s == nil || strings.TrimSpace(profileID) == "" {
		return false
	}
	_, exists := s.totals.Load(trafficScopeForProfile(profileID))
	return exists
}

func (s *TrafficStore) recordAt(profileID string, now time.Time, uploadBytes, downloadBytes uint64) {
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.closed.Load() {
		return
	}
	scope := trafficScopeForProfile(profileID)
	hour := localHourStart(now, s.location).Unix()
	s.addAtomic(trafficScopeAll, hour, now, uploadBytes, downloadBytes)
	if scope != trafficScopeAll {
		s.addAtomic(scope, hour, now, uploadBytes, downloadBytes)
	}
}

func (s *TrafficStore) counterForScope(scope string) *trafficCounter {
	value, _ := s.totals.LoadOrStore(scope, &trafficCounter{})
	return value.(*trafficCounter)
}

func (s *TrafficStore) pendingCounter(key trafficHourKey) *trafficCounter {
	value, _ := s.pending.LoadOrStore(key, &trafficCounter{})
	return value.(*trafficCounter)
}

func (s *TrafficStore) addAtomic(scope string, hour int64, now time.Time, uploadBytes, downloadBytes uint64) {
	nowUnixNano := now.UnixNano()
	total := s.counterForScope(scope)
	total.upload.Add(uploadBytes)
	total.download.Add(downloadBytes)
	storeLatestUnixNano(&total.updatedAt, nowUnixNano)
	pending := s.pendingCounter(trafficHourKey{Scope: scope, Hour: hour})
	pending.upload.Add(uploadBytes)
	pending.download.Add(downloadBytes)
}

func localHourStart(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, location)
}

func encodeHourlyKey(scope string, hour int64) []byte {
	key := make([]byte, len(scope)+1+8)
	copy(key, scope)
	key[len(scope)] = 0
	binary.BigEndian.PutUint64(key[len(scope)+1:], uint64(hour))
	return key
}

func hourlyPrefix(scope string) []byte {
	return append(append([]byte(nil), []byte(scope)...), 0)
}

func decodeTrafficPair(value []byte) (uint64, uint64, error) {
	if len(value) != 16 {
		return 0, 0, fmt.Errorf("无效流量桶长度: %d", len(value))
	}
	return binary.BigEndian.Uint64(value[:8]), binary.BigEndian.Uint64(value[8:]), nil
}

func encodeTrafficPair(upload, download uint64) []byte {
	value := make([]byte, 16)
	binary.BigEndian.PutUint64(value[:8], upload)
	binary.BigEndian.PutUint64(value[8:], download)
	return value
}

func (s *TrafficStore) Flush() error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	return s.flushLocked(time.Now())
}

func (s *TrafficStore) flushLocked(now time.Time) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.closed.Load() {
		return nil
	}
	deltas := make(map[trafficHourKey]trafficValue)
	s.pending.Range(func(key, value any) bool {
		counter := value.(*trafficCounter)
		upload := counter.upload.Swap(0)
		download := counter.download.Swap(0)
		if upload > 0 || download > 0 {
			deltas[key.(trafficHourKey)] = trafficValue{UploadBytes: upload, DownloadBytes: download}
		}
		return true
	})
	if len(deltas) == 0 {
		if s.lastClean.IsZero() || now.Sub(s.lastClean) >= 24*time.Hour {
			return s.cleanupLocked(now)
		}
		return nil
	}
	totals := make(map[string]trafficValue)
	s.totals.Range(func(key, value any) bool {
		totals[key.(string)] = value.(*trafficCounter).snapshot()
		return true
	})
	normalizeOverallTotal(totals)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		totalsBucket := tx.Bucket(trafficTotalsBucket)
		for scope, total := range totals {
			content, err := json.Marshal(total)
			if err != nil {
				return err
			}
			if err := totalsBucket.Put([]byte(scope), content); err != nil {
				return err
			}
		}
		hourlyBucket := tx.Bucket(trafficHourlyBucket)
		for key, delta := range deltas {
			dbKey := encodeHourlyKey(key.Scope, key.Hour)
			var upload, download uint64
			if current := hourlyBucket.Get(dbKey); current != nil {
				var err error
				upload, download, err = decodeTrafficPair(current)
				if err != nil {
					return err
				}
			}
			if err := hourlyBucket.Put(dbKey, encodeTrafficPair(upload+delta.UploadBytes, download+delta.DownloadBytes)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		for key, delta := range deltas {
			counter := s.pendingCounter(key)
			counter.upload.Add(delta.UploadBytes)
			counter.download.Add(delta.DownloadBytes)
		}
		return fmt.Errorf("持久化流量统计失败: %w", err)
	}
	if s.lastClean.IsZero() || now.Sub(s.lastClean) >= 24*time.Hour {
		if err := s.cleanupLocked(now); err != nil {
			return err
		}
	}
	return nil
}

func normalizeOverallTotal(totals map[string]trafficValue) {
	overall := totals[trafficScopeAll]
	overall.UploadBytes = 0
	overall.DownloadBytes = 0
	for scope, total := range totals {
		if scope == trafficScopeAll {
			continue
		}
		overall.UploadBytes += total.UploadBytes
		overall.DownloadBytes += total.DownloadBytes
		if total.UpdatedAt.After(overall.UpdatedAt) {
			overall.UpdatedAt = total.UpdatedAt
		}
	}
	totals[trafficScopeAll] = overall
}

func (s *TrafficStore) cleanupLocked(now time.Time) error {
	cutoff := localHourStart(now.Add(-trafficRetention), s.location).Unix()
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(trafficHourlyBucket)
		cursor := bucket.Cursor()
		var expired [][]byte
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			separator := len(key) - 9
			if separator < 0 || key[separator] != 0 {
				continue
			}
			hour := int64(binary.BigEndian.Uint64(key[separator+1:]))
			if hour < cutoff {
				expired = append(expired, append([]byte(nil), key...))
			}
		}
		for _, key := range expired {
			if err := bucket.Delete(key); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("清理过期流量历史失败: %w", err)
	}
	s.lastClean = now
	return nil
}

func (s *TrafficStore) Summary() TrafficSummary {
	if s == nil {
		return TrafficSummary{}
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	s.samplesMu.Lock()
	defer s.samplesMu.Unlock()
	now := time.Now()
	summary := TrafficSummary{}
	scopes := make([]string, 0)
	s.totals.Range(func(key, _ any) bool {
		scopes = append(scopes, key.(string))
		return true
	})
	sort.Strings(scopes)
	for _, scope := range scopes {
		metrics := s.scopeMetricsLocked(scope, now)
		if metrics.UpdatedAt.After(summary.UpdatedAt) {
			summary.UpdatedAt = metrics.UpdatedAt
		}
		switch {
		case scope == trafficScopeAll:
			summary.Overall = metrics
		case scope == trafficScopeDirect:
			summary.Direct = metrics
		case strings.HasPrefix(scope, trafficProfilePrefix):
			summary.Profiles = append(summary.Profiles, metrics)
		}
	}
	summary.Overall.UploadBytesTotal = summary.Direct.UploadBytesTotal
	summary.Overall.DownloadBytesTotal = summary.Direct.DownloadBytesTotal
	for _, profile := range summary.Profiles {
		summary.Overall.UploadBytesTotal += profile.UploadBytesTotal
		summary.Overall.DownloadBytesTotal += profile.DownloadBytesTotal
	}
	return summary
}

func (s *TrafficStore) scopeMetricsLocked(scope string, now time.Time) TrafficScopeMetrics {
	total := s.counterForScope(scope).snapshot()
	metrics := TrafficScopeMetrics{
		Scope:              scope,
		ProfileID:          profileIDFromTrafficScope(scope),
		UploadBytesTotal:   total.UploadBytes,
		DownloadBytesTotal: total.DownloadBytes,
		LastResetAt:        total.ResetAt,
		UpdatedAt:          total.UpdatedAt,
	}
	if scope == trafficScopeAll || scope == trafficScopeDirect {
		metrics.ProfileID = ""
	}
	last, ok := s.samples[scope]
	if ok {
		elapsed := now.Sub(last.At).Seconds()
		if elapsed > 0 {
			metrics.UploadBps = float64(saturatingSubtract(total.UploadBytes, last.Upload)) / elapsed
			metrics.DownloadBps = float64(saturatingSubtract(total.DownloadBytes, last.Download)) / elapsed
		}
	}
	s.samples[scope] = trafficSpeedSample{At: now, Upload: total.UploadBytes, Download: total.DownloadBytes}
	return metrics
}

func saturatingSubtract(left, right uint64) uint64 {
	if left < right {
		return 0
	}
	return left - right
}

func (s *TrafficStore) ResetAll() error {
	return s.reset(trafficScopeAll)
}

func (s *TrafficStore) ResetProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profileId不能为空")
	}
	return s.reset(trafficScopeForProfile(profileID))
}

func (s *TrafficStore) reset(scope string) error {
	if s == nil {
		return fmt.Errorf("流量数据库未初始化")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed.Load() {
		return fmt.Errorf("流量数据库已关闭")
	}
	now := time.Now()
	if err := s.flushLocked(now); err != nil {
		return err
	}
	nowUnixNano := now.UnixNano()
	previous := make(map[string]trafficValue)
	if scope == trafficScopeAll {
		s.totals.Range(func(key, value any) bool {
			scopeKey := key.(string)
			counter := value.(*trafficCounter)
			previous[scopeKey] = counter.snapshot()
			counter.upload.Store(0)
			counter.download.Store(0)
			counter.resetAt.Store(nowUnixNano)
			counter.updatedAt.Store(nowUnixNano)
			return true
		})
	} else {
		profileCounter := s.counterForScope(scope)
		allCounter := s.counterForScope(trafficScopeAll)
		profileTotal := profileCounter.snapshot()
		all := allCounter.snapshot()
		previous[scope] = profileTotal
		previous[trafficScopeAll] = all
		allCounter.upload.Store(saturatingSubtract(all.UploadBytes, profileTotal.UploadBytes))
		allCounter.download.Store(saturatingSubtract(all.DownloadBytes, profileTotal.DownloadBytes))
		allCounter.updatedAt.Store(nowUnixNano)
		profileCounter.upload.Store(0)
		profileCounter.download.Store(0)
		profileCounter.resetAt.Store(nowUnixNano)
		profileCounter.updatedAt.Store(nowUnixNano)
	}
	totals := make(map[string]trafficValue)
	s.totals.Range(func(key, value any) bool {
		totals[key.(string)] = value.(*trafficCounter).snapshot()
		return true
	})
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(trafficTotalsBucket)
		for key, total := range totals {
			content, err := json.Marshal(total)
			if err != nil {
				return err
			}
			if err := bucket.Put([]byte(key), content); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		for key, value := range previous {
			s.totals.Store(key, newTrafficCounter(value))
		}
		return fmt.Errorf("重置流量统计失败: %w", err)
	}
	s.samplesMu.Lock()
	if scope == trafficScopeAll {
		s.samples = make(map[string]trafficSpeedSample)
	} else {
		delete(s.samples, scope)
		delete(s.samples, trafficScopeAll)
	}
	s.samplesMu.Unlock()
	return nil
}

func (s *TrafficStore) History(scope string, from, to time.Time, groupBy string) (TrafficHistory, error) {
	if s == nil {
		return TrafficHistory{}, fmt.Errorf("流量数据库未初始化")
	}
	scope = strings.TrimSpace(scope)
	if scope != trafficScopeAll && scope != trafficScopeDirect && !strings.HasPrefix(scope, trafficProfilePrefix) {
		return TrafficHistory{}, fmt.Errorf("无效scope: %s", scope)
	}
	groupBy = strings.ToLower(strings.TrimSpace(groupBy))
	if groupBy != "hour" && groupBy != "day" && groupBy != "month" {
		return TrafficHistory{}, fmt.Errorf("无效groupBy: %s", groupBy)
	}
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return TrafficHistory{}, fmt.Errorf("from/to时间范围无效")
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.closed.Load() {
		return TrafficHistory{}, fmt.Errorf("流量数据库已关闭")
	}
	if err := s.flushLocked(time.Now()); err != nil {
		return TrafficHistory{}, err
	}
	points := make(map[int64]TrafficHistoryPoint)
	prefix := hourlyPrefix(scope)
	fromHour := localHourStart(from, s.location).Unix()
	toUnix := to.Unix()
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(trafficHourlyBucket).Cursor()
		seek := encodeHourlyKey(scope, fromHour)
		for key, value := cursor.Seek(seek); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
			hour := int64(binary.BigEndian.Uint64(key[len(prefix):]))
			if hour >= toUnix {
				break
			}
			upload, download, err := decodeTrafficPair(value)
			if err != nil {
				return err
			}
			start := groupTrafficTime(time.Unix(hour, 0).In(s.location), groupBy, s.location)
			point := points[start.Unix()]
			point.Start = start
			point.UploadBytes += upload
			point.DownloadBytes += download
			points[start.Unix()] = point
		}
		return nil
	})
	if err != nil {
		return TrafficHistory{}, fmt.Errorf("查询流量历史失败: %w", err)
	}
	keys := make([]int64, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := TrafficHistory{Scope: scope, GroupBy: groupBy, Timezone: s.location.String(), From: from, To: to, Points: make([]TrafficHistoryPoint, 0, len(keys))}
	for _, key := range keys {
		result.Points = append(result.Points, points[key])
	}
	return result, nil
}

func groupTrafficTime(value time.Time, groupBy string, location *time.Location) time.Time {
	local := value.In(location)
	switch groupBy {
	case "month":
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	case "day":
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	default:
		return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, location)
	}
}

func (s *TrafficStore) Close() error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed.Load() {
		return nil
	}
	flushErr := s.flushLocked(time.Now())
	s.closed.Store(true)
	closeErr := s.db.Close()
	if flushErr != nil {
		if closeErr != nil {
			return fmt.Errorf("%v; 关闭流量数据库失败: %w", flushErr, closeErr)
		}
		return flushErr
	}
	return closeErr
}

type meteredConn struct {
	net.Conn
	store     *TrafficStore
	profileID string
}

func (c *meteredConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		c.store.Record(c.profileID, 0, uint64(n))
	}
	return n, err
}

func (c *meteredConn) Write(buffer []byte) (int, error) {
	n, err := c.Conn.Write(buffer)
	if n > 0 {
		c.store.Record(c.profileID, uint64(n), 0)
	}
	return n, err
}

func (c *meteredConn) CloseWrite() error {
	if writer, ok := c.Conn.(closeWriter); ok {
		return writer.CloseWrite()
	}
	return c.Conn.Close()
}
