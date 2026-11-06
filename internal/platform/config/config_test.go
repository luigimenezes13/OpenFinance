package config_test

import (
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/platform/config"
)

func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
}

func TestLoadOutbox(t *testing.T) {
	t.Run("should default to ten workers and a thirty second job timeout", func(t *testing.T) {
		setRequired(t)
		t.Setenv("OUTBOX_MAX_WORKERS", "")
		t.Setenv("OUTBOX_JOB_TIMEOUT", "")

		configuration, err := config.Load()

		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if configuration.OutboxMaxWorkers != 10 || configuration.OutboxJobTimeout != 30*time.Second {
			t.Errorf("outbox = %d workers, %s timeout", configuration.OutboxMaxWorkers, configuration.OutboxJobTimeout)
		}
	})

	t.Run("should read the workers and the job timeout from the environment", func(t *testing.T) {
		setRequired(t)
		t.Setenv("OUTBOX_MAX_WORKERS", "4")
		t.Setenv("OUTBOX_JOB_TIMEOUT", "2m")

		configuration, err := config.Load()

		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if configuration.OutboxMaxWorkers != 4 || configuration.OutboxJobTimeout != 2*time.Minute {
			t.Errorf("outbox = %d workers, %s timeout", configuration.OutboxMaxWorkers, configuration.OutboxJobTimeout)
		}
	})

	for _, invalid := range []struct{ name, variable, value string }{
		{"should refuse zero workers", "OUTBOX_MAX_WORKERS", "0"},
		{"should refuse workers that are not a number", "OUTBOX_MAX_WORKERS", "ten"},
		{"should refuse a job timeout without a unit", "OUTBOX_JOB_TIMEOUT", "30"},
		{"should refuse a negative job timeout", "OUTBOX_JOB_TIMEOUT", "-5s"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(invalid.variable, invalid.value)

			if _, err := config.Load(); err == nil {
				t.Fatalf("%s=%q was accepted", invalid.variable, invalid.value)
			}
		})
	}
}
