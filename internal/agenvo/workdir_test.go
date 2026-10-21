package agenvo

import (
	"errors"
	"testing"
)

func TestResolveWorkDir(t *testing.T) {
	for _, tt := range []struct{ root, requested, want string }{
		{`C:\Synthetic`, "", `C:\Synthetic`},
		{`C:\Synthetic`, ".", `C:\Synthetic`},
		{`C:\Synthetic`, "a/../b", `C:\Synthetic\b`},
		{`C:\Synthetic`, "中文/child", `C:\Synthetic\中文\child`},
		{`C:\Synthetic`, "c:/synthetic/child", `C:\Synthetic\child`},
		{`D:\Moved`, "child", `D:\Moved\child`},
		{`C:\`, "child", `C:\child`},
		{`\\synthetic-host\share\root`, "child", `\\synthetic-host\share\root\child`},
		{`C:\Synthetic`, "../escape", ""},
		{`C:\Synthetic`, "a/../../escape", ""},
		{`C:\Synthetic`, `C:\Synthetic-other`, ""},
		{`C:\Synthetic`, `C:\Synthetic\..\escape`, ""},
		{`C:\Synthetic`, `D:\Synthetic`, ""},
		{`C:\Synthetic`, `C:child`, ""},
		{`C:\Synthetic`, `\child`, ""},
		{`C:\Synthetic`, `/child`, ""},
		{`C:\Synthetic`, `child:stream`, ""},
		{`C:\Synthetic`, `.. \escape`, ""},
		{`C:\Synthetic`, `child.\escape`, ""},
		{`C:\Synthetic`, `\\?\C:\Synthetic`, ""},
		{`C:\Synthetic`, "child\x00", ""},
		{`C:\Synthetic`, "child\n", ""},
		{"relative-root", "child", ""},
	} {
		t.Run(tt.root+"/"+tt.requested, func(t *testing.T) {
			got, err := resolveWorkDir(tt.root, tt.requested)
			if tt.want == "" {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("got %q error=%v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q error=%v want %q", got, err, tt.want)
			}
		})
	}
}
