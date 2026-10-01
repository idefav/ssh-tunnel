package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// LegacyStore is the historical disk format, before deterministic group migration.
type LegacyStore struct { Version int `json:"version"`; Groups []RouteGroup `json:"groups"`; Routes []RouteRule `json:"routes"` }
type LegacyDomainRule struct {Pattern string `json:"pattern"`;Type string `json:"type"`}

func MigratedGroupID()string{sum:=sha256.Sum256([]byte("ssh-tunnel:routes:v1:migrated-group"));return "group_"+hex.EncodeToString(sum[:16])}
func LegacyRuleID(profileID,kind,pattern string)string{sum:=sha256.Sum256([]byte(profileID+"\x00"+kind+"\x00"+pattern));return "legacy_"+hex.EncodeToString(sum[:16])}

// Migrate preserves desktop v1/v2 and embedded-profile route semantics. It does
// not choose another outlet on conflict, and it never writes or clears the input.
func Migrate(file LegacyStore,embedded map[string][]LegacyDomainRule,profiles map[string]struct{})(RouteStore,bool,[]string,error){
	if file.Version==0 {if len(file.Groups)>0{file.Version=2}else{file.Version=1}}
	if file.Version!=1&&file.Version!=2{return RouteStore{},false,nil,fmt.Errorf("unsupported route version")}
	store:=RouteStore{Version:2,Groups:append([]RouteGroup(nil),file.Groups...)};changed:=file.Version!=2
	group:=func(seed RouteRule)RouteGroup{return RouteGroup{ID:MigratedGroupID(),Name:"未分组（自动迁移）",Enabled:true,Strategy:seed.Strategy,TargetProfileIDs:append([]string(nil),seed.TargetProfileIDs...),Rules:[]RouteRule{}}}
	if file.Version==1{store.Groups=nil;if len(file.Routes)>0{g:=group(file.Routes[0]);g.Rules=append([]RouteRule(nil),file.Routes...);store.Groups=[]RouteGroup{g}}}
	patterns:=map[string]RouteRule{};ids:=map[string]bool{}
	for _,g:=range store.Groups{for _,r:=range g.Rules{patterns[UniquenessKey(r)]=r;ids[r.ID]=true}}
	profileIDs:=make([]string,0,len(embedded));for id:=range embedded{profileIDs=append(profileIDs,id)};sort.Strings(profileIDs)
	var cleared []string
	for _,id:=range profileIDs{
		legacy:=append([]LegacyDomainRule(nil),embedded[id]...);if len(legacy)==0{continue}
		sort.SliceStable(legacy,func(i,j int)bool{return strings.ToLower(strings.TrimSpace(legacy[i].Type))+"\x00"+strings.ToLower(strings.TrimSpace(legacy[i].Pattern))<strings.ToLower(strings.TrimSpace(legacy[j].Type))+"\x00"+strings.ToLower(strings.TrimSpace(legacy[j].Pattern))})
		for _,r:=range legacy{
			kind:=strings.ToLower(strings.TrimSpace(r.Type));if kind==""{kind=GuessConfigRouteType(strings.TrimSpace(r.Pattern))};pattern,err:=NormalizePattern(kind,r.Pattern);if err!=nil{return RouteStore{},false,nil,err}
			candidate:=RouteRule{ID:LegacyRuleID(id,kind,pattern),Pattern:pattern,Type:kind,Enabled:true,Strategy:RouteStrategyFixed,TargetProfileIDs:[]string{id}};key:=UniquenessKey(candidate)
			if old,ok:=patterns[key];ok{if old.ID==candidate.ID{continue};return RouteStore{},false,nil,fmt.Errorf("legacy route conflicts with an existing rule")}
			if ids[candidate.ID]{return RouteStore{},false,nil,fmt.Errorf("legacy route ID conflict")}
			index:=-1;for i,g:=range store.Groups{if g.ID==MigratedGroupID(){index=i;break}};if index<0{store.Groups=append(store.Groups,group(candidate));index=len(store.Groups)-1}
			store.Groups[index].Rules=append(store.Groups[index].Rules,candidate);patterns[key]=candidate;ids[candidate.ID]=true;changed=true
		};cleared=append(cleared,id)
	}
	if store.Groups==nil{store.Groups=[]RouteGroup{}}
	validated,err:=ValidateStore(store,profiles);return validated,changed,cleared,err
}
