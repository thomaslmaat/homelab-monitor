package gameservers

import "testing"

func TestCatalogContainsSupportedGames(t *testing.T) {
	got:=Catalog()
	if len(got)!=3 { t.Fatalf("catalog has %d games, want 3",len(got)) }
	for _,game:=range []string{"minecraft","satisfactory","factorio"} {
		found:=false
		for _,entry:=range got { if entry.Game==game { found=true } }
		if !found { t.Errorf("catalog missing %s",game) }
	}
}

func TestValidateCreateAllowsMoreThanEightGB(t *testing.T) {
	for _,ram:=range []int{10,12,16,20,24} {
		_,err:=ValidateCreate(CreateRequest{Name:"minecraft-1",Game:"minecraft",Version:"1.21.11",RAMGB:ram,StorageGB:20,Port:25565,AcceptEULA:true})
		if err!=nil { t.Errorf("%d GB rejected: %v",ram,err) }
	}
}

func TestValidateCreateRejectsInvalidValues(t *testing.T) {
	cases:=[]CreateRequest{
		{Name:"minecraft-no-eula",Game:"minecraft",Version:"1.21.11",RAMGB:4,StorageGB:10,Port:25565},
		{Name:"Bad Name",Game:"minecraft",Version:"1.21",RAMGB:4,StorageGB:10,Port:25565,AcceptEULA:true},
		{Name:"factorio",Game:"factorio",Version:"latest;rm",RAMGB:4,StorageGB:10,Port:34197},
		{Name:"satisfactory",Game:"satisfactory",Version:"1.0",RAMGB:4,StorageGB:10,Port:7777},
		{Name:"minecraft",Game:"unknown",Version:"1.21",RAMGB:4,StorageGB:10,Port:25565},
	}
	for _,c:=range cases { if _,err:=ValidateCreate(c); err==nil { t.Errorf("invalid request accepted: %#v",c) } }
}
