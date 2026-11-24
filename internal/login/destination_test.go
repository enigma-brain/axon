package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enigma-brain/axon/internal/axondir"
	"github.com/enigma-brain/axon/internal/testkit"
)

// movedCampus is testkit.ExampleConfig with the pathway campus at another API
// server.
var movedCampus = strings.Replace(testkit.ExampleConfig, "https://sky.example.org", "https://elsewhere.example.org", 1)

// movedQuestion is what a refetch that moves campus prints before it asks.
const movedQuestion = "The config file at https://example.org/example.toml changes where the pathway \"campus\" goes:\n" +
	"  now: direct, https://sky.example.org\n" +
	"  new: direct, https://elsewhere.example.org\n"

const destinationPrompt = "Switch to the fetched config file? [y/N, Enter for no]: "

// connectedFromAddress stores testkit.ExampleConfig from the test address, logs in
// on campus, and publishes next at the address.
func connectedFromAddress(t *testing.T, next string) (*harness, *configServer) {
	t.Helper()
	h := newHarness(t)
	server := h.serveConfigs()
	h.storeFrom("/example.toml", testkit.ExampleConfig)
	server.publish("/example.toml", testkit.ExampleConfig)
	h.connectedTo("campus")
	server.publish("/example.toml", next)
	return h, server
}

// keptCampus checks that the stored copy is testkit.ExampleConfig and that the
// login carried on with campus as it was.
func keptCampus(t *testing.T, h *harness, before os.FileInfo) {
	t.Helper()
	if h.stored("example") != testkit.ExampleConfig {
		t.Errorf("stored file = %q, want the stored copy kept whole", h.stored("example"))
	}
	if h.active() != "campus" || h.record().SkyEndpoint != "https://sky.example.org" {
		t.Errorf("active = %q, record = %+v; want campus as it was", h.active(), h.record())
	}
	if !os.SameFile(before, h.emailFile()) {
		t.Error("the login on the stored settings was disconnected")
	}
}

func TestRefetchMovingThePathwayAsksAndNoKeepsTheStoredCopy(t *testing.T) {
	for name, answer := range map[string]string{"enter": "\n", "no": "n\n"} {
		t.Run(name, func(t *testing.T) {
			h, _ := connectedFromAddress(t, movedCampus)
			h.terminal()
			before := h.emailFile()
			if err := h.login(Options{In: strings.NewReader(answer)}); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			path := filepath.Join(h.home, ".axon", "configs", "example.toml")
			want := movedQuestion + destinationPrompt + "Kept the stored copy of " + path + ".\n"
			if !strings.HasPrefix(h.out.String(), want) {
				t.Errorf("output:\n%s\nwant it to start:\n%s", h.out.String(), want)
			}
			keptCampus(t, h, before)
			if h.errOut.Len() != 0 {
				t.Errorf("warnings = %q", h.errOut.String())
			}
		})
	}
}

func TestRefetchMovingThePathwayWithoutATerminalKeepsTheStoredCopy(t *testing.T) {
	h, _ := connectedFromAddress(t, movedCampus)
	before := h.emailFile()
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	want := "axon: the config file at https://example.org/example.toml changes where the pathway \"campus\" goes, and there is no terminal to ask whether to switch. Using the stored file. Run `axon login --replace` to switch.\n"
	if h.errOut.String() != want {
		t.Errorf("warnings = %q, want %q", h.errOut.String(), want)
	}
	if !strings.HasPrefix(h.out.String(), movedQuestion) {
		t.Errorf("output lacks the old and the new values:\n%s", h.out.String())
	}
	keptCampus(t, h, before)
}

// Input that is not a terminal is not read for the answer: a piped yes
// keeps the stored copy, as no terminal does.
func TestRefetchMovingThePathwayIgnoresPipedAnswers(t *testing.T) {
	h, _ := connectedFromAddress(t, movedCampus)
	before := h.emailFile()
	if err := h.login(Options{In: strings.NewReader("y\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if strings.Contains(h.out.String(), destinationPrompt) || !strings.Contains(h.errOut.String(), "Run `axon login --replace` to switch.") {
		t.Errorf("output:\n%s\nwarnings: %q", h.out.String(), h.errOut.String())
	}
	keptCampus(t, h, before)
}

func TestRefetchMovingThePathwayWithReplaceSwitches(t *testing.T) {
	h, _ := connectedFromAddress(t, movedCampus)
	if err := h.login(Options{Replace: true}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.HasPrefix(h.out.String(), movedQuestion) || strings.Contains(h.out.String(), destinationPrompt) {
		t.Errorf("output:\n%s\nwant the values printed and no question", h.out.String())
	}
	if h.stored("example") != movedCampus || h.record().SkyEndpoint != "https://elsewhere.example.org" {
		t.Errorf("stored = %q, record = %+v; want the fetched file used", h.stored("example"), h.record())
	}
}

func TestRefetchMovingThePathwayYesSwitches(t *testing.T) {
	h, _ := connectedFromAddress(t, movedCampus)
	h.terminal()
	if err := h.login(Options{In: strings.NewReader("y\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if !strings.HasPrefix(h.out.String(), movedQuestion+destinationPrompt) {
		t.Errorf("output:\n%s", h.out.String())
	}
	if h.stored("example") != movedCampus || h.record().SkyEndpoint != "https://elsewhere.example.org" {
		t.Errorf("stored = %q, record = %+v; want the fetched file used", h.stored("example"), h.record())
	}
}

// Each value of where a pathway goes is asked about: the kind, the
// address, the proxy and the application.
func TestRefetchAsksForEveryValueOfWhereThePathwayGoes(t *testing.T) {
	teleportCampus := strings.Replace(testkit.ExampleConfig, "kind = \"direct\"", "kind = \"teleport\"", 1)
	teleportCampus = strings.Replace(teleportCampus, "[pathway.sky]\nendpoint = \"https://sky.example.org\"", "[pathway.teleport]\nproxy = \"teleport.example.org\"\n\n[pathway.sky]\napp = \"skypilot\"", 1)
	for name, tc := range map[string]struct {
		pathway, next, now, new string
	}{
		"kind":    {"campus", teleportCampus, "direct, https://sky.example.org", "teleport, proxy teleport.example.org, application skypilot"},
		"address": {"campus", movedCampus, "direct, https://sky.example.org", "direct, https://elsewhere.example.org"},
		"proxy": {"remote", strings.Replace(testkit.ExampleConfig, `proxy = "teleport.example.org"`, `proxy = "teleport.example.org:3080"`, 1),
			"teleport, proxy teleport.example.org, application skypilot", "teleport, proxy teleport.example.org:3080, application skypilot"},
		"application": {"remote", strings.Replace(testkit.ExampleConfig, `app = "skypilot"`, `app = "other"`, 1),
			"teleport, proxy teleport.example.org, application skypilot", "teleport, proxy teleport.example.org, application other"},
	} {
		t.Run(name, func(t *testing.T) {
			var h *harness
			if tc.pathway == "remote" {
				h = newTeleportHarness(t).harness
			} else {
				h = newHarness(t)
			}
			server := h.serveConfigs()
			h.noFiles()
			h.terminal()
			h.storeFrom("/example.toml", testkit.ExampleConfig)
			server.publish("/example.toml", tc.next)
			if err := axondir.New(h.home).SetPathway(tc.pathway); err != nil {
				t.Fatal(err)
			}
			if err := h.login(Options{In: strings.NewReader("\n")}); err != nil {
				t.Fatalf("login: %v\n%s", err, h.out.String())
			}
			want := "The config file at https://example.org/example.toml changes where the pathway \"" + tc.pathway + "\" goes:\n" +
				"  now: " + tc.now + "\n  new: " + tc.new + "\n" + destinationPrompt
			if !strings.HasPrefix(h.out.String(), want) {
				t.Errorf("output:\n%s\nwant it to start:\n%s", h.out.String(), want)
			}
			if h.stored("example") != testkit.ExampleConfig {
				t.Error("the stored copy changed")
			}
		})
	}
}

// Addresses that name the same server, by the rule for clashes, are no
// change, and a change elsewhere in the file is stored without a question.
func TestRefetchKeepingWhereThePathwayGoesIsStored(t *testing.T) {
	next := strings.Replace(testkit.ExampleConfig, `endpoint = "https://sky.example.org"`, `endpoint = "https://SKY.example.org/"`, 1)
	next = strings.Replace(next, `app = "skypilot"`, `app = "other"`, 1)
	h, _ := connectedFromAddress(t, next)
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if strings.Contains(h.out.String(), "changes where") || h.errOut.Len() != 0 {
		t.Errorf("output:\n%s\nwarnings: %q", h.out.String(), h.errOut.String())
	}
	if h.stored("example") != next {
		t.Error("the fetched file was not stored")
	}
}

func TestRefetchDroppingThePathwayAsksAndNoKeepsTheStoredCopy(t *testing.T) {
	h, _ := connectedFromAddress(t, dropCampus)
	h.terminal()
	before := h.emailFile()
	if err := h.login(Options{In: strings.NewReader("\n")}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	want := "The config file at https://example.org/example.toml changes where the pathway \"campus\" goes:\n" +
		"  now: direct, https://sky.example.org\n" +
		"  new: no longer in the file\n" + destinationPrompt
	if !strings.HasPrefix(h.out.String(), want) {
		t.Errorf("output:\n%s\nwant it to start:\n%s", h.out.String(), want)
	}
	keptCampus(t, h, before)
}

// The question about where the pathway goes comes before the one about a
// lower revision; no to the first asks nothing more, and --replace answers
// both.
func TestRefetchAsksAboutTheServerBeforeTheRevision(t *testing.T) {
	withRevision := func(data, n string) string {
		return strings.Replace(data, "format = 2\n", "format = 2\nrevision = "+n+"\n", 1)
	}
	setup := func(t *testing.T) *harness {
		h := newHarness(t)
		server := h.serveConfigs()
		h.storeFrom("/example.toml", withRevision(testkit.ExampleConfig, "5"))
		server.publish("/example.toml", withRevision(testkit.ExampleConfig, "5"))
		h.connectedTo("campus")
		h.terminal()
		server.publish("/example.toml", withRevision(movedCampus, "4"))
		return h
	}

	t.Run("no", func(t *testing.T) {
		h := setup(t)
		if err := h.login(Options{In: strings.NewReader("n\n")}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(h.out.String(), "which is older") {
			t.Errorf("asked about the revision after no:\n%s", h.out.String())
		}
		if h.stored("example") != withRevision(testkit.ExampleConfig, "5") {
			t.Error("the stored copy changed")
		}
	})
	t.Run("yes, then yes", func(t *testing.T) {
		h := setup(t)
		if err := h.login(Options{In: strings.NewReader("y\ny\n")}); err != nil {
			t.Fatal(err)
		}
		out := h.out.String()
		if i, j := strings.Index(out, destinationPrompt), strings.Index(out, "which is older"); i < 0 || j < i {
			t.Errorf("output:\n%s\nwant the server question first", out)
		}
		if h.stored("example") != withRevision(movedCampus, "4") {
			t.Error("the fetched file was not stored")
		}
	})
	t.Run("replace", func(t *testing.T) {
		h := setup(t)
		if err := h.login(Options{Replace: true}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(h.out.String(), "[y/N") {
			t.Errorf("asked with --replace:\n%s", h.out.String())
		}
		if h.stored("example") != withRevision(movedCampus, "4") {
			t.Error("the fetched file was not stored")
		}
	})
}

// A file the user adds with --config that moves the pathway in use is
// stored as before, without a question, and the old and the new values are
// printed.
func TestConfigMovingThePathwayInUsePrintsTheValues(t *testing.T) {
	h := newHarness(t)
	h.connectedTo("campus")
	file := filepath.Join(t.TempDir(), "example.toml")
	if err := os.WriteFile(file, []byte(movedCampus), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.login(Options{Config: file}); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	want := "The config file changes where the pathway \"campus\" goes:\n" +
		"  now: direct, https://sky.example.org\n" +
		"  new: direct, https://elsewhere.example.org\n"
	if !strings.HasPrefix(h.out.String(), want) || strings.Contains(h.out.String(), "[y/N") {
		t.Errorf("output:\n%s\nwant it to start:\n%s", h.out.String(), want)
	}
	if h.stored("example") != movedCampus || h.record().SkyEndpoint != "https://elsewhere.example.org" {
		t.Errorf("stored = %q, record = %+v", h.stored("example"), h.record())
	}
}

// axon login deletes a record whose stored file is gone, and files a put
// left renamed aside.
func TestLoginDeletesARecordWithoutItsFile(t *testing.T) {
	h := newHarness(t)
	h.store(otherConfig)
	h.connectedTo("campus")
	records := filepath.Join(h.home, ".axon", "state", "configs")
	if err := os.Remove(filepath.Join(h.home, ".axon", "configs", "other.toml")); err != nil {
		t.Fatal(err)
	}
	aside := filepath.Join(h.home, ".axon", "configs", "gone.toml.removing")
	if err := os.WriteFile(aside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.run(""); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	for _, p := range []string{filepath.Join(records, "other.toml"), aside} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(records, "example.toml")); err != nil {
		t.Errorf("the record of a stored file is gone: %v", err)
	}
}

// The user name the server reports is printed without control or format
// characters.
func TestConnectedLineIsCleaned(t *testing.T) {
	h := newHarness(t)
	h.user = "ada\u200b[2J@example.org\u202e"
	h.logIn()
	if err := h.run("campus"); err != nil {
		t.Fatalf("login: %v\n%s", err, h.out.String())
	}
	if out := h.out.String(); strings.ContainsAny(out, "\u200b\u202e") || !strings.Contains(out, "sky: connected as ada[2J@example.org (SkyPilot") {
		t.Errorf("output = %q", out)
	}
}
