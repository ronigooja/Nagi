package runtimecontrol

import (
 "strings"
 "testing"
)
func TestModeReadAndSetPreservesConfig(t *testing.T){
 data:=[]byte("mixed-port: 7890\nmode: rule\nrules:\n  - MATCH,DIRECT\n")
 mode,err:=Mode(data);if err!=nil||mode!="rule"{t.Fatalf("mode=%q err=%v",mode,err)}
 updated,err:=SetMode(data,"global");if err!=nil||!strings.Contains(string(updated),"mode: global")||!strings.Contains(string(updated),"mixed-port: 7890"){t.Fatalf("updated=%s err=%v",updated,err)}
}
func TestModeDefaultsAndRejectsInvalid(t *testing.T){mode,err:=Mode([]byte("proxies: []\n"));if err!=nil||mode!="rule"{t.Fatalf("default=%q err=%v",mode,err)};if _,err:=SetMode([]byte("- bad\n"),"global");err==nil{t.Fatal("invalid root accepted")}}
