package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thomaslmaat/homelab-monitor/internal/gameservers"
)

const gameNamespace = "game-servers"
const gameRegistryName = "homelab-game-server-registry"

type managedGameServer struct {
	gameservers.Config
	Version string `json:"version"`
	RAMGB int `json:"ram_gb"`
	StorageGB int `json:"storage_gb"`
	Port int `json:"port"`
	NodePort int `json:"node_port,omitempty"`
}

type gameRegistry struct { Data map[string]string `json:"data"` }

func (s *Server) gameCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"games": gameservers.Catalog(), "ram_options_gb": []int{2,4,6,8,10,12,14,16,18,20,24,32,48,64}, "storage_class":"longhorn"})
}

func (s *Server) readManagedGames(ctx context.Context) ([]managedGameServer, error) {
	var cm struct { Data map[string]string `json:"data"` }
	err := s.kube.GetResource(ctx, "/api/v1/namespaces/"+gameNamespace+"/configmaps/"+gameRegistryName, &cm)
	if err != nil { return nil, err }
	raw := cm.Data["servers.json"]
	if strings.TrimSpace(raw)=="" { return []managedGameServer{},nil }
	var rows []managedGameServer
	if err=json.Unmarshal([]byte(raw),&rows); err!=nil { return nil,fmt.Errorf("game registry is invalid: %w",err) }
	return rows,nil
}

func (s *Server) saveManagedGames(ctx context.Context, rows []managedGameServer) error {
	raw,err:=json.Marshal(rows); if err!=nil{return err}
	cm:=map[string]any{"apiVersion":"v1","kind":"ConfigMap","metadata":map[string]any{"name":gameRegistryName,"namespace":gameNamespace,"labels":map[string]string{"app.kubernetes.io/managed-by":"homelab-monitor"}},"data":map[string]string{"servers.json":string(raw)}}
	path:="/api/v1/namespaces/"+gameNamespace+"/configmaps/"+gameRegistryName
	var current map[string]any
	if err=s.kube.GetResource(ctx,path,&current); err==nil { return s.kube.PutResource(ctx,path,cm) }
	return s.kube.CreateResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/configmaps",cm)
}

func (s *Server) ensureGameNamespace(ctx context.Context) error {
	var ns map[string]any
	if err:=s.kube.GetResource(ctx,"/api/v1/namespaces/"+gameNamespace,&ns); err==nil{return nil}
	return s.kube.CreateResource(ctx,"/api/v1/namespaces",map[string]any{"apiVersion":"v1","kind":"Namespace","metadata":map[string]any{"name":gameNamespace,"labels":map[string]string{"app.kubernetes.io/managed-by":"homelab-monitor"}}})
}

func (s *Server) managedGameInventory(w http.ResponseWriter,r *http.Request) {
	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second);defer cancel()
	if s.kube==nil {writeJSON(w,503,map[string]string{"error":"Kubernetes client unavailable"});return}
	rows,err:=s.readManagedGames(ctx)
	if err!=nil {
		// A not-yet-created registry is an empty inventory; all other failures are surfaced.
		if !strings.Contains(err.Error(),"404") {writeJSON(w,502,map[string]string{"error":"Could not read game server registry: "+err.Error()});return}
		rows=[]managedGameServer{}
	}
	writeJSON(w,200,map[string]any{"configured":true,"game_servers":rows})
}

func (s *Server) createManagedGame(w http.ResponseWriter,r *http.Request) {
	var req gameservers.CreateRequest
	r.Body=http.MaxBytesReader(w,r.Body,8192);defer r.Body.Close()
	if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil {writeJSON(w,400,map[string]string{"error":"Invalid request"});return}
	name,err:=gameservers.ValidateCreate(req);if err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
	req.Name=name;req.Game=strings.ToLower(strings.TrimSpace(req.Game));req.Version=strings.TrimSpace(req.Version)
	ctx,cancel:=context.WithTimeout(r.Context(),30*time.Second);defer cancel()
	if s.kube==nil {writeJSON(w,503,map[string]string{"error":"Kubernetes client unavailable"});return}
	s.gameMu.Lock();defer s.gameMu.Unlock()
	if err=s.ensureGameNamespace(ctx);err!=nil {writeJSON(w,502,map[string]string{"error":"Could not create game namespace: "+err.Error()});return}
	rows,err:=s.readManagedGames(ctx);if err!=nil && !strings.Contains(err.Error(),"404") {writeJSON(w,502,map[string]string{"error":"Could not read game registry: "+err.Error()});return}
	for _,x:=range rows {if x.ID==name {writeJSON(w,409,map[string]string{"error":"A server with this name already exists"});return}}
	template:=gameservers.Template{}
	for _,x:=range gameservers.Catalog(){if x.Game==req.Game{template=x;break}}
	if template.Game=="" {writeJSON(w,400,map[string]string{"error":"Unsupported game"});return}
	if req.Port==0 {req.Port=template.DefaultPort}
	id:=name
	image:=template.Image
	env:=[]any{}
	containerPorts:=[]any{}
	volumeMount:=map[string]any{"name":"data","mountPath":"/data"}
	switch req.Game {
	case "minecraft":
		env=[]any{envVar("EULA","TRUE"),envVar("TYPE","PAPER"),envVar("VERSION",req.Version),envVar("MEMORY",strconv.Itoa(req.RAMGB)+"G")}
		containerPorts=[]any{port("game",req.Port,"TCP")}
		volumeMount["mountPath"]="/data"
	case "satisfactory":
		env=[]any{envVar("MAXPLAYERS","4"),envVar("SERVERGAMEPORT",strconv.Itoa(req.Port)),envVar("SERVERMESSAGINGPORT","8888"),envVar("SERVERBEACONPORT","15000"),envVar("PGID","1000"),envVar("PUID","1000"),envVar("STEAMBETA","false")}
		containerPorts=[]any{port("game-udp",req.Port,"UDP"),port("game-tcp",req.Port,"TCP"),port("messaging",8888,"UDP"),port("beacon",15000,"UDP")}
		volumeMount["mountPath"]="/config"
	case "factorio":
		env=[]any{envVar("VERSION",req.Version)}
		containerPorts=[]any{port("game",req.Port,"UDP"),port("rcon",27015,"TCP")}
		volumeMount["mountPath"]="/factorio"
	}
	labels:=map[string]string{"app.kubernetes.io/managed-by":"homelab-monitor","homelab-monitor.io/game-server":id,"homelab-monitor.io/game":req.Game}
	pvc:=map[string]any{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":map[string]any{"name":id+"-data","namespace":gameNamespace,"labels":labels},"spec":map[string]any{"accessModes":[]string{"ReadWriteOnce"},"storageClassName":"longhorn","resources":map[string]any{"requests":map[string]string{"storage":strconv.Itoa(req.StorageGB)+"Gi"}}}}
	dep:=map[string]any{"apiVersion":"apps/v1","kind":"Deployment","metadata":map[string]any{"name":id,"namespace":gameNamespace,"labels":labels},"spec":map[string]any{"replicas":0,"selector":map[string]any{"matchLabels":labels},"template":map[string]any{"metadata":map[string]any{"labels":labels},"spec":map[string]any{"containers":[]any{map[string]any{"name":req.Game,"image":image+":"+req.Version,"imagePullPolicy":"IfNotPresent","env":env,"ports":containerPorts,"resources":map[string]any{"requests":map[string]string{"cpu":"500m","memory":strconv.Itoa(req.RAMGB)+"Gi"},"limits":map[string]string{"memory":strconv.Itoa(req.RAMGB)+"Gi"}},"volumeMounts":[]any{volumeMount}}},"volumes":[]any{map[string]any{"name":"data","persistentVolumeClaim":map[string]any{"claimName":id+"-data"}}}}}}}
	if err=s.kube.CreateResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims",pvc);err!=nil {writeJSON(w,502,map[string]string{"error":"Could not create Longhorn PVC: "+err.Error()});return}
	if err=s.kube.CreateResource(ctx,"/apis/apps/v1/namespaces/"+gameNamespace+"/deployments",dep);err!=nil {_=s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+id+"-data");writeJSON(w,502,map[string]string{"error":"Could not create Deployment: "+err.Error()});return}
	svcPorts:=[]any{}
	for _,p:=range containerPorts {entry:=p.(map[string]any);svcPorts=append(svcPorts,map[string]any{"name":entry["name"],"port":entry["containerPort"],"targetPort":entry["containerPort"],"protocol":entry["protocol"]})}
	svc:=map[string]any{"apiVersion":"v1","kind":"Service","metadata":map[string]any{"name":id,"namespace":gameNamespace,"labels":labels},"spec":map[string]any{"type":"NodePort","selector":labels,"ports":svcPorts}}
	if err=s.kube.CreateResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/services",svc);err!=nil {_=s.kube.DeleteResource(ctx,"/apis/apps/v1/namespaces/"+gameNamespace+"/deployments/"+id);_=s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+id+"-data");writeJSON(w,502,map[string]string{"error":"Could not create Service: "+err.Error()});return}
	row:=managedGameServer{Config:gameservers.Config{ID:id,Name:req.Name,Game:req.Game,Namespace:gameNamespace,Workload:id,WorkloadType:"deployment"},Version:req.Version,RAMGB:req.RAMGB,StorageGB:req.StorageGB,Port:req.Port}
	rows=append(rows,row)
	if err=s.saveManagedGames(ctx,rows);err!=nil {log.Printf("game registry save failed: %v",err);writeJSON(w,502,map[string]string{"error":"Resources created but registry could not be saved: "+err.Error()});return}
	log.Printf("game server created: user=%q id=%q game=%q",sessionUser(r),id,req.Game)
	writeJSON(w,201,map[string]any{"game_server":row,"status":"created"})
}

func (s *Server) deleteManagedGame(w http.ResponseWriter,r *http.Request) {
	id:=r.PathValue("id");keepData:=r.URL.Query().Get("keep_data")!="false"
	ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel()
	if s.kube==nil {writeJSON(w,503,map[string]string{"error":"Kubernetes client unavailable"});return}
	s.gameMu.Lock();defer s.gameMu.Unlock()
	rows,err:=s.readManagedGames(ctx);if err!=nil {writeJSON(w,502,map[string]string{"error":"Could not read registry"});return}
	index:=-1
	for i,x:=range rows {if x.ID==id {index=i;break}}
	if index<0 {writeJSON(w,404,map[string]string{"error":"Game server not found"});return}
	_ = s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/services/"+url.PathEscape(id))
	if err=s.kube.DeleteResource(ctx,"/apis/apps/v1/namespaces/"+gameNamespace+"/deployments/"+url.PathEscape(id));err!=nil {writeJSON(w,502,map[string]string{"error":"Could not delete Deployment: "+err.Error()});return}
	if !keepData {if err=s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+url.PathEscape(id)+"-data");err!=nil {writeJSON(w,502,map[string]string{"error":"Deployment deleted but PVC removal failed: "+err.Error()});return}}
	rows=append(rows[:index],rows[index+1:]...)
	if err=s.saveManagedGames(ctx,rows);err!=nil {writeJSON(w,502,map[string]string{"error":"Server removed but registry update failed: "+err.Error()});return}
	log.Printf("game server deleted: user=%q id=%q keep_data=%t",sessionUser(r),id,keepData)
	writeJSON(w,200,map[string]any{"status":"deleted","data_preserved":keepData})
}

func envVar(name,value string) map[string]any {return map[string]any{"name":name,"value":value}}
func port(name string,n int,protocol string) map[string]any {return map[string]any{"name":name,"containerPort":n,"protocol":protocol}}

var _ sync.Locker
