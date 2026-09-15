package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstanceStatus verifies that instance status values are correctly
// assigned and retrieved for various status types.
func TestInstanceStatus(t *testing.T) {
	statuses := []string{"RUNNING", "STOPPED", "STARTING", "DELETING", "ERROR"}

	for _, status := range statuses {
		instance := &api.Instance{
			ID:     "test-instance",
			Status: status,
		}
		assert.Equal(t, status, instance.Status)
	}
}

// TestInstanceFields verifies that all instance fields are correctly
// assigned and can be retrieved with their expected values.
func TestInstanceFields(t *testing.T) {
	ip := "192.168.1.100"
	instance := &api.Instance{
		ID:        "test-instance",
		UUID:      "uuid-123",
		Name:      "Test Instance",
		Status:    "RUNNING",
		IP:        &ip,
		CPUCores:  "8",
		Memory:    "32GB",
		Storage:   100,
		GPUType:   "a6000",
		NumGPUs:   "1",
		Template:  "ubuntu-22.04",
		CreatedAt: "2023-10-01T10:00:00Z",
		Port:      22,
		K8s:       false,
		Promoted:  false,
	}

	assert.Equal(t, "test-instance", instance.ID)
	assert.Equal(t, "uuid-123", instance.UUID)
	assert.Equal(t, "Test Instance", instance.Name)
	assert.Equal(t, "RUNNING", instance.Status)
	assert.Equal(t, "192.168.1.100", instance.GetIP())
	assert.Equal(t, "8", instance.CPUCores)
	assert.Equal(t, "32GB", instance.Memory)
	assert.Equal(t, 100, instance.Storage)
	assert.Equal(t, "a6000", instance.GPUType)
	assert.Equal(t, "1", instance.NumGPUs)
	assert.Equal(t, "ubuntu-22.04", instance.Template)
	assert.Equal(t, "2023-10-01T10:00:00Z", instance.CreatedAt)
	assert.Equal(t, 22, instance.Port)
	assert.False(t, instance.K8s)
	assert.False(t, instance.Promoted)
}

// func TestStatusCommandValidation(t *testing.T) {
// 	t.Skip("Skipping status command validation test - Args function issues")
// }

func TestRunStatus(t *testing.T) {
	previousInteractive := statusIsInteractive
	statusIsInteractive = func() bool { return false }
	t.Cleanup(func() { statusIsInteractive = previousInteractive })
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/instances/list", r.URL.Path)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			t.Setenv("TNR_API_TOKEN", "test-token")
			t.Setenv("TNR_API_URL", server.URL)
			err := RunStatus()
			if status == http.StatusOK {
				require.NoError(t, err)
			} else {
				var apiErr *api.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
			}
		})
	}
}
