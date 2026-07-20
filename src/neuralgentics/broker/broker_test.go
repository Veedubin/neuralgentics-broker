package broker

import (
	"testing"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/launcher"
	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

func TestHealth_StoppedServer(t *testing.T) {
	b := NewBroker()

	err := b.RegisterServer(types.ServerConfig{
		Name:    "fake-server",
		Command: "echo",
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	health := b.Health()
	status, ok := health["fake-server"]
	if !ok {
		t.Fatal("expected fake-server in health map")
	}
	if status != string(launcher.HealthStopped) {
		t.Errorf("expected health status %q, got %q", launcher.HealthStopped, status)
	}
}

func TestHealth_MultipleServers(t *testing.T) {
	b := NewBroker()

	servers := []types.ServerConfig{
		{Name: "server-a", Command: "echo", Type: "stdio"},
		{Name: "server-b", Command: "echo", Type: "stdio"},
	}
	for _, s := range servers {
		if err := b.RegisterServer(s); err != nil {
			t.Fatalf("RegisterServer %q failed: %v", s.Name, err)
		}
	}

	health := b.Health()
	if len(health) != 2 {
		t.Fatalf("expected 2 entries in health map, got %d", len(health))
	}
	for _, s := range servers {
		status, ok := health[s.Name]
		if !ok {
			t.Errorf("expected %q in health map", s.Name)
			continue
		}
		if status != string(launcher.HealthStopped) {
			t.Errorf("expected %q to be %q, got %q", s.Name, launcher.HealthStopped, status)
		}
	}
}

func TestHealth_NoServers(t *testing.T) {
	b := NewBroker()

	health := b.Health()
	if len(health) != 0 {
		t.Errorf("expected empty health map for no servers, got %d entries", len(health))
	}
}

func TestHealthDeep_StoppedServer(t *testing.T) {
	b := NewBroker()

	err := b.RegisterServer(types.ServerConfig{
		Name:    "fake-server",
		Command: "echo",
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	health := b.HealthDeep()
	status, ok := health["fake-server"]
	if !ok {
		t.Fatal("expected fake-server in health map")
	}
	if status != string(launcher.HealthStopped) {
		t.Errorf("expected health status %q, got %q", launcher.HealthStopped, status)
	}
}

func TestHealthDeep_NoServers(t *testing.T) {
	b := NewBroker()

	health := b.HealthDeep()
	if len(health) != 0 {
		t.Errorf("expected empty health map for no servers, got %d entries", len(health))
	}
}

func TestHealth_ReturnsAllStatuses(t *testing.T) {
	// Verify that Health returns one entry per registered server.
	b := NewBroker()

	names := []string{"s1", "s2", "s3"}
	for _, n := range names {
		if err := b.RegisterServer(types.ServerConfig{
			Name: n, Command: "true", Type: "stdio",
		}); err != nil {
			t.Fatalf("RegisterServer %q failed: %v", n, err)
		}
	}

	health := b.Health()
	if len(health) != len(names) {
		t.Errorf("expected %d health entries, got %d", len(names), len(health))
	}
	for _, n := range names {
		if _, ok := health[n]; !ok {
			t.Errorf("expected server %q in health map", n)
		}
	}
}
