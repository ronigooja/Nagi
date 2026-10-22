package menu

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPromptUTF8ErasureAndTerminalRestore(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		interrupt         bool
		wantErr           error
	}{
		{name: "Chinese", input: "中 得\x7f\n", want: "中"},
		{name: "mixed", input: "A中B得\x7f\x7f\x7f文\n", want: "A文"},
		{name: "erase all", input: "中文\x7f\x7f\x7fX\n", want: "X"},
		{name: "literal values", input: "0 b q\n", want: "0 b q"},
		{name: "cancel", interrupt: true, wantErr: ErrBack},
		{name: "EOF", input: "\x04", wantErr: ErrQuit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			before.Iflag &^= unix.IUTF8
			if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, before); err != nil {
				t.Fatal(err)
			}
			s := NewSession(&fakeRunner{}, slave, io.Discard)
			s.interrupts = make(chan os.Signal, 1)
			done := make(chan struct{})
			var value string
			var readErr error
			go func() {
				value, readErr = s.Prompt("Group name")
				close(done)
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
				if err != nil {
					t.Fatal(err)
				}
				if state.Iflag&unix.IUTF8 != 0 {
					break
				}
				if time.Now().After(deadline) {
					s.interrupts <- os.Interrupt
					<-done
					t.Fatal("prompt did not enable UTF-8 erasure")
				}
				time.Sleep(time.Millisecond)
			}
			if tc.interrupt {
				s.interrupts <- os.Interrupt
			} else if _, err := master.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				s.interrupts <- os.Interrupt
				<-done
				t.Fatal("prompt did not finish")
			}
			if value != tc.want || readErr != tc.wantErr {
				t.Fatalf("Prompt = (%q, %v), want (%q, %v)", value, readErr, tc.want, tc.wantErr)
			}
			after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if *after != *before {
				t.Fatalf("terminal settings not restored: before=%+v after=%+v", before, after)
			}
		})
	}
}
