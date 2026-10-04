package engine

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)
func TestFollowStreamsAppendedLines(t *testing.T){
 dir:=t.TempDir();paths:=nagiruntime.Paths{RuntimeDir:dir,LogPath:filepath.Join(dir,"mihomo.log"),LockPath:filepath.Join(dir,"lock"),SocketPath:filepath.Join(dir,"sock"),PIDPath:filepath.Join(dir,"pid")};if err:=os.WriteFile(paths.LogPath,[]byte("old\n"),0600);err!=nil{t.Fatal(err)};m,err:=New(Options{Binary:"true",ConfigPath:filepath.Join(dir,"config"),Paths:paths});if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel();got:=make(chan string,1);go func(){err:=m.Follow(ctx,func(line string)error{got<-line;cancel();return nil});if err!=context.Canceled{t.Errorf("follow err=%v",err)}}();time.Sleep(80*time.Millisecond);f,_:=os.OpenFile(paths.LogPath,os.O_APPEND|os.O_WRONLY,0600);_,_=f.WriteString("new\n");_=f.Close();select{case line:=<-got:if !strings.Contains(line,"new"){t.Fatalf("line=%q",line)};case <-time.After(time.Second):t.Fatal("follow timeout")}
}
