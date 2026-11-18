package gameservers

import (
	"fmt"
	"regexp"
	"strings"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}package gameservers

import (
	"fmt"
	"regexp"
	"strings"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var versionPattern = regexp.MustCompile()

type CreateRequest struct {
	Name string `json:"name"`
	Game string `json:"game"`
	Version string `json:"version"`
	RAMGB int `json:"ram_gb"`
	StorageGB int `json:"storage_gb"`
	Port int `json:"port"`
	AcceptEULA bool `json:"accept_eula"`
}

type Template struct {
	Game string `json:"game"`
	DisplayName string `json:"display_name"`
	DefaultPort int `json:"default_port"`
	Ports []int `json:"ports"`
	StorageGB int `json:"storage_gb"`
	MinRAMGB int `json:"min_ram_gb"`
	Image string `json:"image"`
	VersionHint string `json:"version_hint"`
}

var templates = map[string]Template{
	"minecraft": {Game:"minecraft",DisplayName:"Minecraft Java (Paper)",DefaultPort:25565,Ports:[]int{25565},StorageGB:10,MinRAMGB:2,Image:"itzg/minecraft-server",VersionHint:"Minecraft release (Paper)"},
	"satisfactory": {Game:"satisfactory",DisplayName:"Satisfactory",DefaultPort:7777,Ports:[]int{7777,7777,8888},StorageGB:20,MinRAMGB:8,Image:"wolveix/satisfactory-server",VersionHint:"stable of experimental"},
	"factorio": {Game:"factorio",DisplayName:"Factorio",DefaultPort:34197,Ports:[]int{34197,27015},StorageGB:10,MinRAMGB:2,Image:"factoriotools/factorio",VersionHint:"stable of stable-x.y.z"},
}

func Catalog() []Template {
	return []Template{templates["minecraft"],templates["satisfactory"],templates["factorio"]}
}

func ValidateCreate(r CreateRequest) (string,error) {
	r.Name=strings.ToLower(strings.TrimSpace(r.Name))
	r.Game=strings.ToLower(strings.TrimSpace(r.Game))
	r.Version=strings.TrimSpace(r.Version)
	if !dnsLabel.MatchString(r.Name) || len(r.Name)>40 { return "",fmt.Errorf("naam moet een Kubernetes-naam zijn (maximaal 40 tekens)") }
	t,ok:=templates[r.Game]; if !ok { return "",fmt.Errorf("game moet minecraft, satisfactory of factorio zijn") }
	if !versionPattern.MatchString(r.Version) { return "",fmt.Errorf("ongeldige of ontbrekende gameversie") }
	if r.Game=="minecraft" && !r.AcceptEULA { return "",fmt.Errorf("bevestig eerst de Minecraft EULA") }
	if r.Game=="satisfactory" && r.Version!="stable" && r.Version!="experimental" { return "",fmt.Errorf("Satisfactory ondersteunt hier stable of experimental; exacte Steam-builds worden door de image bijgewerkt") }
	if r.RAMGB<t.MinRAMGB || r.RAMGB>64 { return "",fmt.Errorf("RAM moet tussen %d en 64 GB liggen",t.MinRAMGB) }
	if r.StorageGB<1 || r.StorageGB>500 { return "",fmt.Errorf("opslag moet tussen 1 en 500 GB liggen") }
	if r.Port<1024 || r.Port>65535 { return "",fmt.Errorf("poort moet tussen 1024 en 65535 liggen") }
	_ = t
	return r.Name,nil
}
