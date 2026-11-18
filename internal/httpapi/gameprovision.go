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
	ExternalPorts []int `json:"external_ports,omitempty"`
	State string `json:"state,omitempty"`
	Ready int `json:"ready"`
	Desired int `json:"desired"`
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
	if err:=s.kube.GetResource(ctx,"/api/v1/namespaces/"+gameNamespace,&ns); err!=nil { return fmt.Errorf("namespace %q is not ready; sync the Homelab Monitor ArgoCD application first: %w",gameNamespace,err) }
	return nil
}

func (s *Server) managedGameInventory(w http.ResponseWriter,r *http.Request) {
	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second);defer cancel()
	if s.kube==nil {writeJSON(w,503,map[string]string{"error":"Kubernetes client unavailable"});return}
	rows,err:=s.readManagedGames(ctx)
	if err!=nil {
		if !strings.Contains(err.Error(),"404") {writeJSON(w,502,map[string]string{"error":"Could not read game server registry: "+err.Error()});return}
		rows=[]managedGameServer{}
	}
	cluster:=s.kube.Overview(ctx)
	for i:=range rows {
		rows[i].State="not_found"
		if cluster.Available { for _,d:=range cluster.Deployments { if d.Namespace==rows[i].Namespace && d.Name==rows[i].Workload { rows[i].Ready=d.Ready;rows[i].Desired=d.Desired;switch {case d.Desired==0:rows[i].State="stopped";case d.Ready>0:rows[i].State="running";default:rows[i].State="starting"};break } } }
		var svc struct { Spec struct { Ports []struct { NodePort int `json:"nodePort"` } `json:"ports"` } `json:"spec"` }
		if err:=s.kube.GetResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/services/"+rows[i].ID,&svc);err==nil { for _,p:=range svc.Spec.Ports { if p.NodePort>0 {rows[i].ExternalPorts=append(rows[i].ExternalPorts,p.NodePort)} } }
	}
	writeJSON(w,200,map[string]any{"configured":true,"game_servers":rows,"cluster_available":cluster.Available})
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
	imageTag:="latest"
	env:=[]any{}
	containerPorts:=[]any{}
	volumeMount:=map[string]any{"name":"data","mountPath":"/data"}
	switch req.Game {
	case "minecraft":
		imageTag="java21"
		env=[]any{envVar("EULA","TRUE"),envVar("TYPE","PAPER"),envVar("VERSION",req.Version),envVar("MEMORY",strconv.Itoa(max(1,req.RAMGB-1))+"G")}
		containerPorts=[]any{port("game",req.Port,"TCP")}
		volumeMount["mountPath"]="/data"
	case "satisfactory":
		beta:="false";if req.Version=="experimental"{beta="true"}
		env=[]any{envVar("MAXPLAYERS","4"),envVar("SERVERGAMEPORT",strconv.Itoa(req.Port)),envVar("SERVERMESSAGINGPORT","8888"),envVar("PGID","1000"),envVar("PUID","1000"),envVar("STEAMBETA",beta)}
		containerPorts=[]any{port("game-udp",req.Port,"UDP"),port("game-tcp",req.Port,"TCP"),port("messaging",8888,"TCP")}
		volumeMount["mountPath"]="/config"
	case "factorio":
		imageTag=req.Version
		env=[]any{envVar("PORT",strconv.Itoa(req.Port)),envVar("RCON_PORT","27015")}
		containerPorts=[]any{port("game",req.Port,"UDP"),port("rcon",27015,"TCP")}
		volumeMount["mountPath"]="/factorio"
	}
	labels:=map[string]string{"app.kubernetes.io/managed-by":"homelab-monitor","homelab-monitor.io/game-server":id,"homelab-monitor.io/game":req.Game}
	pvc:=map[string]any{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":map[string]any{"name":id+"-data","namespace":gameNamespace,"labels":labels},"spec":map[string]any{"accessModes":[]string{"ReadWriteOnce"},"storageClassName":"longhorn","resources":map[string]any{"requests":map[string]string{"storage":strconv.Itoa(req.StorageGB)+"Gi"}}}}
	dep:=map[string]any{"apiVersion":"apps/v1","kind":"Deployment","metadata":map[string]any{"name":id,"namespace":gameNamespace,"labels":labels},"spec":map[string]any{"replicas":0,"selector":map[string]any{"matchLabels":labels},"template":map[string]any{"metadata":map[string]any{"labels":labels},"spec":map[string]any{"containers":[]any{map[string]any{"name":req.Game,"image":image+":"+imageTag,"imagePullPolicy":"IfNotPresent","env":env,"ports":containerPorts,"resources":map[string]any{"requests":map[string]string{"cpu":"500m","memory":strconv.Itoa(req.RAMGB)+"Gi"},"limits":map[string]string{"memory":strconv.Itoa(req.RAMGB)+"Gi"}},"volumeMounts":[]any{volumeMount}}},"volumes":[]any{map[string]any{"name":"data","persistentVolumeClaim":map[string]any{"claimName":id+"-data"}}}}}}}
	if err=s.kube.CreateResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims",pvc);err!=nil {writeJSON(w,502,map[string]string{"error":"Could not create Longhorn PVC: "+err.Error()});return}
	if err=s.kube.CreateResource(ctx,"/apis/apps/v1/namespaces/"+gameNamespace+"/deployments",dep);err!=nil {_=s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+id+"-data");writeJSON(w,502,map[string]string{"error":"Could not create Deployment: "+err.Error()});return}
	svcPorts:=[]any{}
	for _,p:=range containerPorts {entry:=p.(map[string]any);svcPorts=append(svcPorts,map[string]any{"name":entry["name"],"port":entry["containerPort"],"targetPort":entry["containerPort"],"protocol":entry["protocol"]})}
	svc:=map[string]any{"apiVersion":"v1","kind":"Service","metadata":map[string]any{"name":id,"namespace":gameNamespace,"labels":labels},"spec":map[string]any{"type":"NodePort","selector":labels,"ports":svcPorts}}
	if err=s.kube.CreateResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/services",svc);err!=nil {_=s.kube.DeleteResource(ctx,"/apis/apps/v1/namespaces/"+gameNamespace+"/deployments/"+id);_=s.kube.DeleteResource(ctx,"/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+id+"-data");writeJSON(w,502,map[string]string{"error":"Could not create Service: "+err.Error()});return}
	row:=managedGameServer{Config:gameservers.Config{ID:id,Name:req.Name,Game:req.Game,Namespace:gameNamespace,Workload:id,WorkloadType:"deployment"},Version:req.Version,RAMGB:req.RAMGB,StorageGB:req.StorageGB,Port:req.Port}
	rows=append(rows,row)
	if err=s.saveManagedGames(ctx,rows);err!=nil {log.Printf("game registry save failed: %v",err);writeJSON(w,502,map[string]string{"error":"Resources created but registry could not be saved: "+err.Error()});return}
	log.Printf("game server created: user=%q id=%q game=%q",s.sessionUser(r),id,req.Game)
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
	log.Printf("game server deleted: user=%q id=%q keep_data=%t",s.sessionUser(r),id,keepData)
	writeJSON(w,200,map[string]any{"status":"deleted","data_preserved":keepData})
}

func envVar(name,value string) map[string]any {return map[string]any{"name":name,"value":value}}
func port(name string,n int,protocol string) map[string]any {return map[string]any{"name":name,"containerPort":n,"protocol":protocol}}

type updateGameRequest struct {
	Version string `json:"version"`
	RAMGB int `json:"ram_gb"`
	StorageGB int `json:"storage_gb"`
	Port int `json:"port"`
}

func (s *Server) updateManagedGame(w http.ResponseWriter,r *http.Request) {
	id:=r.PathValue("id")
	r.Body=http.MaxBytesReader(w,r.Body,8192);defer r.Body.Close()
	var req updateGameRequest
	if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil {writeJSON(w,400,map[string]string{"error":"Invalid request"});return}
	ctx,cancel:=context.WithTimeout(r.Context(),25*time.Second);defer cancel()
	if s.kube==nil {writeJSON(w,503,map[string]string{"error":"Kubernetes client unavailable"});return}
	s.gameMu.Lock();defer s.gameMu.Unlock()
	rows,err:=s.readManagedGames(ctx);if err!=nil {writeJSON(w,502,map[string]string{"error":"Could not read registry: "+err.Error()});return}
	idx:=-1
	for i:=range rows {if rows[i].ID==id {idx=i;break}}
	if idx<0 {writeJSON(w,404,map[string]string{"error":"Game server not found"});return}
	old:=rows[idx]
	if req.Port==0 {req.Port=old.Port}
	if req.StorageGB<old.StorageGB {writeJSON(w,400,map[string]string{"error":"Opslag kan alleen worden vergroot; verkleinen kan data beschadigen"});return}
	if _,err=gameservers.ValidateCreate(gameservers.CreateRequest{Name:old.Name,Game:old.Game,Version:req.Version,RAMGB:req.RAMGB,StorageGB:req.StorageGB,Port:req.Port,AcceptEULA:true});err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
	template:=gameservers.Template{}
	for _,x:=range gameservers.Catalog(){if x.Game==old.Game {template=x;break}}
	imageTag:="latest"
	env:=[]any{}
	ports:=[]any{}
	switch old.Game {
	case "minecraft":
		imageTag="java21"
		env=[]any{envVar("EULA","TRUE"),envVar("TYPE","PAPER"),envVar("VERSION",req.Version),envVar("MEMORY",strconv.Itoa(max(1,req.RAMGB-1))+"G")}
		ports=[]any{port("game",req.Port,"TCP")}
	case "satisfactory":
		beta:="false";if req.Version=="experimental"{beta="true"}
		env=[]any{envVar("MAXPLAYERS","4"),envVar("SERVERGAMEPORT",strconv.Itoa(req.Port)),envVar("SERVERMESSAGINGPORT","8888"),envVar("PGID","1000"),envVar("PUID","1000"),envVar("STEAMBETA",beta)}
		ports=[]any{port("game-udp",req.Port,"UDP"),port("game-tcp",req.Port,"TCP"),port("messaging",8888,"TCP")}
	case "factorio":
		imageTag=req.Version
		env=[]any{envVar("PORT",strconv.Itoa(req.Port)),envVar("RCON_PORT","27015")}
		ports=[]any{port("game",req.Port,"UDP"),port("rcon",27015,"TCP")}
	}
	deploymentPath:="/apis/apps/v1/namespaces/"+gameNamespace+"/deployments/"+id
	var deployment map[string]any
	if err=s.kube.GetResource(ctx,deploymentPath,&deployment);err!=nil {writeJSON(w,502,map[string]string{"error":"Could not read Deployment: "+err.Error()});return}
	depSpec,_:=deployment["spec"].(map[string]any);templateSpec,_:=depSpec["template"].(map[string]any);podSpec,_:=templateSpec["spec"].(map[string]any);containers,_:=podSpec["containers"].([]any)
	updatedContainer:=false
	for _,raw:=range containers {container,ok:=raw.(map[string]any);if !ok || container["name"]!=old.Game {continue};container["image"]=template.Image+":"+imageTag;container["env"]=env;container["ports"]=ports;container["resources"]=map[string]any{"requests":map[string]string{"cpu":"500m","memory":strconv.Itoa(req.RAMGB)+"Gi"},"limits":map[string]string{"memory":strconv.Itoa(req.RAMGB)+"Gi"}};updatedContainer=true}
	if !updatedContainer {writeJSON(w,502,map[string]string{"error":"Game container not found in Deployment"});return}
	if err=s.kube.PutResource(ctx,deploymentPath,deployment);err!=nil {writeJSON(w,502,map[string]string{"error":"Could not update Deployment: "+err.Error()});return}
	if req.StorageGB>old.StorageGB {
		pvcPath:="/api/v1/namespaces/"+gameNamespace+"/persistentvolumeclaims/"+id+"-data"
		pvcPatch:=map[string]any{"spec":map[string]any{"resources":map[string]any{"requests":map[string]string{"storage":strconv.Itoa(req.StorageGB)+"Gi"}}}}
		if err=s.kube.PatchResource(ctx,pvcPath,pvcPatch);err!=nil {writeJSON(w,502,map[string]string{"error":"Deployment updated but Longhorn volume resize failed: "+err.Error()});return}
	}
	servicePorts:=[]any{}
	for _,raw:=range ports {p:=raw.(map[string]any);servicePorts=append(servicePorts,map[string]any{"name":p["name"],"port":p["containerPort"],"targetPort":p["containerPort"],"protocol":p["protocol"]})}
	servicePath:="/api/v1/namespaces/"+gameNamespace+"/services/"+id
	var service map[string]any
	if err=s.kube.GetResource(ctx,servicePath,&service);err!=nil {writeJSON(w,502,map[string]string{"error":"Deployment updated but Service could not be read: "+err.Error()});return}
	spec,ok:=service["spec"].(map[string]any);if !ok {writeJSON(w,502,map[string]string{"error":"Service has an invalid spec"});return}
	oldPorts,_:=spec["ports"].([]any)
	for _,np:=range servicePorts {next:=np.(map[string]any);for _,op:=range oldPorts {previous,ok:=op.(map[string]any);if ok && previous["name"]==next["name"] {if nodePort,exists:=previous["nodePort"];exists {next["nodePort"]=nodePort}}}}
	spec["ports"]=servicePorts
	if err=s.kube.PutResource(ctx,servicePath,service);err!=nil {writeJSON(w,502,map[string]string{"error":"Deployment updated but Service port update failed: "+err.Error()});return}
	rows[idx].Version=req.Version;rows[idx].RAMGB=req.RAMGB;rows[idx].StorageGB=req.StorageGB;rows[idx].Port=req.Port
	if err=s.saveManagedGames(ctx,rows);err!=nil {writeJSON(w,502,map[string]string{"error":"Server updated but registry save failed: "+err.Error()});return}
	log.Printf("game server configured: user=%q id=%q game=%q",s.sessionUser(r),id,old.Game)
	writeJSON(w,200,map[string]any{"status":"updated","game_server":rows[idx]})
}
