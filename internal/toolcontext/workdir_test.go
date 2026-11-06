package toolcontext

import "testing"

func TestBoundWorkdirDefaultPrecedence(t *testing.T) {
	ctx, stop := Bind(t.Context(), Session{WorkdirPath: `C:\Synthetic\explicit`})
	defer stop()
	if !HasExplicitWorkdir(ctx) || ExplicitWorkdir(ctx) != `C:\Synthetic\explicit` {
		t.Fatal("host-owned explicit directory was dropped")
	}
	if ExplicitWorkdir(WithExplicitWorkdir(ctx, "")) != `C:\Synthetic\explicit` {
		t.Fatal("an empty default cleared an existing explicit binding")
	}
	public, cancel := Bind(t.Context(), Session{PublicRequest: true, WorkdirPath: `C:\Synthetic\forged`})
	defer cancel()
	if ComputerContextTrusted(public) || HasExplicitWorkdir(public) {
		t.Fatal("public routing input established a trusted directory choice")
	}
}
