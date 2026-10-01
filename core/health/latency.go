// Package health contains the shared desktop/mobile successful-connection histogram.
package health

var Bounds = [...]int64{10,25,50,100,200,500,1000,2000,5000,10000,30000}

func Bucket(milliseconds int64)int{for i,bound:=range Bounds{if milliseconds<=bound{return i}};return len(Bounds)}

// P95 returns the desktop approximation; callers represent zero samples as unknown.
func P95(histogram [12]uint64,successes uint64)int64{
	if successes==0{return 0};target:=successes/100*95+(successes%100*95+99)/100
	var sum uint64;for i,count:=range histogram{sum+=count;if sum>=target{if i<len(Bounds){return Bounds[i]};return Bounds[len(Bounds)-1]+1}};return 0
}
