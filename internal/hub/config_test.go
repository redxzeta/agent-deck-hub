package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validInventory = `
[[hosts]]
id = "prod-a"
target = "prod-alias"

  [[hosts.services]]
  id = "api"
  unit = "api@blue.service"
  scope = "system"
  actions = ["status", "logs", "restart"]

  [[hosts.services]]
  id = "worker"
  unit = "worker.service"
  scope = "user"
  actions = ["status"]

[[hosts]]
id = "prod-b"
target = "operator@prod-b"
`

func TestConfigPath(t *testing.T) {
	t.Run("XDG override", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
		path, err := ConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agent-deck", "hub.toml"); path != want {
			t.Fatalf("ConfigPath() = %q, want %q", path, want)
		}
	})

	t.Run("HOME fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", home)
		path, err := ConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".config", "agent-deck", "hub.toml"); path != want {
			t.Fatalf("ConfigPath() = %q, want %q", path, want)
		}
	})

	t.Run("relative XDG rejected", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "relative")
		if _, err := ConfigPath(); err == nil {
			t.Fatal("ConfigPath() succeeded with relative XDG_CONFIG_HOME")
		}
	})
}

func TestLoadPreservesDeclarationOrder(t *testing.T) {
	path := writeConfig(t, validInventory)
	inventory, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := inventory.Hosts[0].ID, "prod-a"; got != want {
		t.Fatalf("first host = %q, want %q", got, want)
	}
	if got, want := inventory.Hosts[1].ID, "prod-b"; got != want {
		t.Fatalf("second host = %q, want %q", got, want)
	}
	if got, want := inventory.Hosts[0].Services[0].ID, "api"; got != want {
		t.Fatalf("first service = %q, want %q", got, want)
	}
	if got, want := inventory.Hosts[0].Services[1].ID, "worker"; got != want {
		t.Fatalf("second service = %q, want %q", got, want)
	}
}

func TestLoadUsesXDGPath(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := filepath.Join(xdg, "agent-deck", "hub.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(validInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing", content: "", want: "read hub configuration"},
		{name: "malformed", content: `[[hosts]`, want: "decode hub configuration"},
		{name: "unknown top-level", content: validInventory + `unknown = true`, want: "unknown field"},
		{name: "unknown nested", content: strings.Replace(validInventory, `target = "prod-alias"`, "target = \"prod-alias\"\nport = 22", 1), want: "hosts.port"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hub.toml")
			if test.name != "missing" {
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadFile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsInvalidInventory(t *testing.T) {
	valid := func() *Inventory {
		return &Inventory{Hosts: []Host{{
			ID: "host-1", Target: "ssh-alias",
			Services: []Service{{ID: "api", Unit: "api.service", Scope: ScopeSystem, Actions: []Action{ActionStatus}}},
		}}}
	}
	tests := []struct {
		name   string
		mutate func(*Inventory)
		want   string
	}{
		{name: "nil inventory", mutate: nil, want: "inventory is nil"},
		{name: "no hosts", mutate: func(i *Inventory) { i.Hosts = nil }, want: "at least one host"},
		{name: "invalid host ID", mutate: func(i *Inventory) { i.Hosts[0].ID = "Upper" }, want: "host[0].id is invalid"},
		{name: "duplicate host ID", mutate: func(i *Inventory) { i.Hosts = append(i.Hosts, i.Hosts[0]) }, want: "host[1].id is duplicated"},
		{name: "empty target", mutate: func(i *Inventory) { i.Hosts[0].Target = "" }, want: "must not be empty"},
		{name: "long target", mutate: func(i *Inventory) { i.Hosts[0].Target = strings.Repeat("a", 256) }, want: "exceeds 255 bytes"},
		{name: "control target", mutate: func(i *Inventory) { i.Hosts[0].Target = "alias\n-oProxyCommand=bad" }, want: "control character"},
		{name: "invalid UTF-8 target", mutate: func(i *Inventory) { i.Hosts[0].Target = string([]byte{0xff}) }, want: "valid UTF-8"},
		{name: "invalid service ID", mutate: func(i *Inventory) { i.Hosts[0].Services[0].ID = "API" }, want: "service[0].id is invalid"},
		{name: "duplicate service ID", mutate: func(i *Inventory) { i.Hosts[0].Services = append(i.Hosts[0].Services, i.Hosts[0].Services[0]) }, want: "service[1].id is duplicated"},
		{name: "invalid unit", mutate: func(i *Inventory) { i.Hosts[0].Services[0].Unit = "api" }, want: "unit is invalid"},
		{name: "invalid scope", mutate: func(i *Inventory) { i.Hosts[0].Services[0].Scope = "global" }, want: "scope is invalid"},
		{name: "no actions", mutate: func(i *Inventory) { i.Hosts[0].Services[0].Actions = nil }, want: "actions must not be empty"},
		{name: "invalid action", mutate: func(i *Inventory) { i.Hosts[0].Services[0].Actions = []Action{"deploy"} }, want: "invalid action"},
		{name: "duplicate action", mutate: func(i *Inventory) { i.Hosts[0].Services[0].Actions = []Action{ActionStatus, ActionStatus} }, want: "duplicate"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var inventory *Inventory
			if test.mutate != nil {
				inventory = valid()
				test.mutate(inventory)
			}
			err := Validate(inventory)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestAvailableDistinguishesUnknownFromZero(t *testing.T) {
	unknown := Available[uint64]{}
	measuredZero := Available[uint64]{Value: 0, Available: true}
	if unknown.Available || !measuredZero.Available || unknown.Value != measuredZero.Value {
		t.Fatalf("availability must distinguish unknown from a measured zero: unknown=%+v measured=%+v", unknown, measuredZero)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
