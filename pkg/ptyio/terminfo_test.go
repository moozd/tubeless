package ptyio

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSetupTerminfoCompilesUndercurlCaps locks in the whole point of
// shipping tubeless's own terminfo entry: an app doing a real terminfo
// lookup against TERM=tubeless (via TERMINFO_DIRS, the same env pair a
// spawned shell gets) finds Smulx (curly underline) and Setulc
// (independent underline color) — the two capabilities plain
// xterm-256color lacks, and the whole reason this entry exists.
func TestSetupTerminfoCompilesUndercurlCaps(t *testing.T) {
	if _, err := exec.LookPath("tic"); err != nil {
		t.Skip("tic not installed")
	}
	if _, err := exec.LookPath("infocmp"); err != nil {
		t.Skip("infocmp not installed")
	}

	term, extraEnv := setupTerminfo()
	if term != terminfoTerm {
		t.Fatalf("term = %q, want %q (tic must have failed — check stderr above)", term, terminfoTerm)
	}
	if len(extraEnv) != 1 || !strings.HasPrefix(extraEnv[0], "TERMINFO_DIRS=") {
		t.Fatalf("extraEnv = %v, want a single TERMINFO_DIRS entry", extraEnv)
	}
	dirs := strings.TrimPrefix(extraEnv[0], "TERMINFO_DIRS=")

	cmd := exec.Command("infocmp", "-x", term)
	cmd.Env = append(os.Environ(), "TERMINFO_DIRS="+dirs)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("infocmp %s (TERMINFO_DIRS=%s): %v", term, dirs, err)
	}

	for _, capName := range []string{"Tc", "Smulx=", "Setulc="} {
		if !strings.Contains(string(out), capName) {
			t.Errorf("infocmp -x %s output missing %q:\n%s", term, capName, out)
		}
	}

	// smcup (alt-screen) comes only from the "use=xterm-256color" chain,
	// not anything declared directly in tubeless.terminfo — confirms the
	// entry actually inherited xterm-256color's capabilities rather than
	// compiling down to just the three extensions above.
	if !strings.Contains(string(out), "smcup=") {
		t.Errorf("infocmp -x %s output doesn't look like it inherited xterm-256color (no smcup):\n%s", term, out)
	}
}

// TestAugmentTerminfoAddsUndercurlCaps locks in setupTerminfo's second
// job: patching a multiplexer's own terminfo entry (see augmentedTerms'
// doc comment) rather than tubeless's own "tubeless" name, since that's
// what a program running inside tmux/screen actually looks up.
func TestAugmentTerminfoAddsUndercurlCaps(t *testing.T) {
	if _, err := exec.LookPath("tic"); err != nil {
		t.Skip("tic not installed")
	}
	if _, err := exec.LookPath("infocmp"); err != nil {
		t.Skip("infocmp not installed")
	}

	for _, name := range augmentedTerms {
		t.Run(name, func(t *testing.T) {
			if err := exec.Command("infocmp", name).Run(); err != nil {
				t.Skipf("no system %s terminfo entry: %v", name, err)
			}

			dir := t.TempDir()
			if err := augmentTerminfo(dir, name); err != nil {
				t.Fatalf("augmentTerminfo(%s): %v", name, err)
			}

			cmd := exec.Command("infocmp", "-x", name)
			cmd.Env = append(os.Environ(), "TERMINFO_DIRS="+dir+":")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("infocmp %s (TERMINFO_DIRS=%s): %v", name, dir, err)
			}
			for _, capName := range []string{"Smulx=", "Setulc="} {
				if !strings.Contains(string(out), capName) {
					t.Errorf("infocmp -x %s output missing %q:\n%s", name, capName, out)
				}
			}
		})
	}
}
