package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type settingsFixtureReader struct {
	responses map[string]string
}

func (r settingsFixtureReader) Call(_ context.Context, method string, _ any, out any) error {
	response, ok := r.responses[method]
	if !ok {
		return errors.New("Authorization: Bearer private-fixture-error")
	}
	return json.Unmarshal([]byte(response), out)
}

func TestCodexSettingsDoesNotExposeNativeCredentialsOrErrorDetails(t *testing.T) {
	reader := settingsFixtureReader{responses: map[string]string{
		"account/read":            `{"account":{"type":"chatgpt","planType":"pro","email":"private-fixture-email"},"requiresOpenaiAuth":true}`,
		"config/read":             `{"config":{"model":"native-default","approval_policy":"on-request","instructions":"private-fixture-instructions","model_providers":{"custom":{"api_key":"private-fixture-key"}},"mcp_servers":{"private":{"env":{"TOKEN":"private-fixture-env"}}}},"layers":[{"config":{"secret":"private-fixture-layer"}}]}`,
		"configRequirements/read": `{"requirements":{"allowedSandboxModes":[],"modelProviders":{"custom":{"api_key":"private-fixture-managed"}},"additionalDeveloperInstructions":"private-fixture-policy"}}`,
		"skills/list":             `{"data":[{"cwd":"/repo","skills":[{"name":"task-worker","description":"API_KEY=private-fixture-description","path":"private-fixture-path","scope":"repo","enabled":true,"dependencies":{"tools":[{"value":"private-fixture-dependency"}]}}],"errors":[{"message":"private-fixture-skill-error"}]}]}`,
		"mcpServerStatus/list":    `{"data":[{"name":"native-tools","authStatus":"oAuth","httpOrigin":"private-fixture-origin","tools":{"lookup":{"inputSchema":{"secret":"private-fixture-schema"}}},"toolsError":"private-fixture-mcp-error"}],"nextCursor":null}`,
		"permissionProfile/list":  `{"data":[{"id":":read-only","description":"Authorization: Bearer private-fixture-profile","allowed":true}],"nextCursor":null}`,
	}}

	settings := readCodexSettings(context.Background(), reader, "/repo", true)
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-fixture") {
		t.Fatalf("native credentials, instruction data, or error details reached the client: %s", raw)
	}
	if settings.ConfigStatus != "ready" || settings.RateLimits.Status != "unavailable" {
		t.Fatalf("optional rate-limit failure hid available native defaults: %+v", settings)
	}
	if settings.Managed.AllowedSandboxModes == nil || len(*settings.Managed.AllowedSandboxModes) != 0 {
		t.Fatal("an explicit managed deny-all list must stay distinct from an absent restriction")
	}
	if len(settings.Skills.Skills) != 1 || settings.Skills.Skills[0].Name != "task-worker" {
		t.Fatal("credential filtering hid ordinary skill metadata")
	}
}
