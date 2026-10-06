package firestore

import "testing"

// Test_instanceName pins the writer's name in the signals document: the Cloud Run
// revision, else the job execution, else the host, with the process id.
func Test_instanceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		host string
		want string
	}{
		{name: "a Cloud Run service names its revision", env: map[string]string{"K_REVISION": "app-00018-vtx", "K_SERVICE": "app"}, host: "localhost", want: "app-00018-vtx/1"},
		{name: "a Cloud Run job names its execution", env: map[string]string{"CLOUD_RUN_EXECUTION": "migrate-v0-4-0-qn6zr", "CLOUD_RUN_JOB": "migrate"}, host: "localhost", want: "migrate-v0-4-0-qn6zr/1"},
		{name: "a revision wins over an execution", env: map[string]string{"K_REVISION": "rev", "CLOUD_RUN_EXECUTION": "exec"}, host: "h", want: "rev/1"},
		{name: "an empty variable counts as unset", env: map[string]string{"K_REVISION": ""}, host: "dev-box", want: "dev-box/1"},
		{name: "elsewhere the host", env: map[string]string{}, host: "dev-box", want: "dev-box/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lookup := func(key string) (string, bool) {
				v, ok := tt.env[key]

				return v, ok
			}
			if got := instanceName(lookup, tt.host, 1); got != tt.want {
				t.Errorf("instanceName() = %q, want %q", got, tt.want)
			}
		})
	}
}
