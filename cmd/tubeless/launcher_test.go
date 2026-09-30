package main

import (
	"reflect"
	"testing"
)

func TestParseDesktopEntryVisibility(t *testing.T) {
	entry := func(extra string) string {
		return "[Desktop Entry]\nType=Application\nName=Calc\nExec=calc --new %U\n" + extra
	}
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"plain app", entry(""), true},
		{"hidden from menus", entry("NoDisplay=true\n"), false},
		{"terminal program", entry("Terminal=true\n"), false},
		{"other desktop only", entry("OnlyShowIn=GNOME;\n"), false},
		{"this desktop only", entry("OnlyShowIn=Hyprland;\n"), true},
		{"missing binary", entry("TryExec=/no/such/binary\n"), false},
		{"not an application", "[Desktop Entry]\nType=Link\nName=x\nExec=x\n", false},
	}
	for _, c := range cases {
		_, ok := parseDesktopEntry(c.text, "calc", "Hyprland")
		if ok != c.want {
			t.Errorf("%s: visible = %v, want %v", c.name, ok, c.want)
		}
	}
}

func TestParseDesktopEntryIgnoresOtherGroups(t *testing.T) {
	text := "[Desktop Entry]\nType=Application\nName=App\nExec=app\n[Desktop Action new]\nName=Other\nExec=other\n"
	e, ok := parseDesktopEntry(text, "app", "")
	if !ok || e.Name != "App" || !reflect.DeepEqual(e.Argv, []string{"app"}) {
		t.Errorf("got %+v, %v", e, ok)
	}
}

func TestSplitExec(t *testing.T) {
	cases := map[string][]string{
		`foo --bar %U`:               {"foo", "--bar"},
		`"/opt/my app/run" -x "a b"`: {"/opt/my app/run", "-x", "a b"},
		`env FOO=1 app 100%%`:        {"env", "FOO=1", "app", "100%"},
		`app "say \"hi\"" %f`:        {"app", `say "hi"`},
		`   spaced   out  `:          {"spaced", "out"},
	}
	for in, want := range cases {
		if got := splitExec(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitExec(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFuzzyScorePrefersBoundariesAndRejectsMissing(t *testing.T) {
	if fuzzyScore("fx", "Firefox") < 0 {
		t.Error("subsequence should match")
	}
	if fuzzyScore("xz", "Firefox") >= 0 {
		t.Error("out-of-order letters must not match")
	}
	if fuzzyScore("fi", "Firefox") <= fuzzyScore("fi", "Notifier Fi") {
		t.Error("prefix match should outrank a later one")
	}
}

func TestRankAppsUsesRecentsAsTieBreakAndOrdersEmptyQuery(t *testing.T) {
	apps := []appEntry{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}, {ID: "c", Name: "Gamma"}}
	got := rankApps(apps, "", []string{"c", "b"})
	if ids := appIDs(got); !reflect.DeepEqual(ids, []string{"c", "b", "a"}) {
		t.Errorf("empty query order = %v", ids)
	}
	got = rankApps(apps, "a", []string{"c"})
	if ids := appIDs(got); ids[0] != "a" && ids[0] != "c" {
		t.Errorf("query a ranked %v", ids)
	}
	if len(rankApps(apps, "zzz", nil)) != 0 {
		t.Error("no-match query should list nothing")
	}
}

func appIDs(apps []appEntry) []string {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ID
	}
	return ids
}
