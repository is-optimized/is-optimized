package cloudflare

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEdgeProjectOwnsOnlyTheExistingBrowserCheckSetting(t *testing.T) {
	data, err := os.ReadFile("Pulumi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Name    string `yaml:"name"`
		Runtime string `yaml:"runtime"`
		Config  map[string]struct {
			Type    string `yaml:"type"`
			Default any    `yaml:"default"`
		} `yaml:"config"`
		Resources map[string]struct {
			Type       string         `yaml:"type"`
			Properties map[string]any `yaml:"properties"`
			Options    struct {
				Version string `yaml:"version"`
				Protect bool   `yaml:"protect"`
				Retain  bool   `yaml:"retainOnDelete"`
			} `yaml:"options"`
		} `yaml:"resources"`
	}
	if err := yaml.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if project.Name != "is-optimized-edge" || project.Runtime != "yaml" {
		t.Fatal("edge project coupled to another lifecycle")
	}
	if len(project.Config) != 1 || project.Config["zoneId"].Type != "string" || project.Config["zoneId"].Default != nil {
		t.Fatal("zone must be explicitly configured without provider credentials in source")
	}
	if len(project.Resources) != 1 {
		t.Fatal("edge project must manage only one setting")
	}
	resource, ok := project.Resources["browserIntegrityCheck"]
	if !ok {
		t.Fatal("expected setting resource missing")
	}
	if resource.Type != "cloudflare:ZoneSetting" || len(resource.Properties) != 3 || resource.Properties["zoneId"] != "${zoneId}" || resource.Properties["settingId"] != "browser_check" || resource.Properties["value"] != "off" {
		t.Fatalf("unexpected resource boundary: %+v", resource)
	}
	if resource.Options.Version != "6.20.0" || !resource.Options.Protect || !resource.Options.Retain {
		t.Fatal("provider version or adoption safety options changed")
	}
	var example struct {
		Config map[string]string `yaml:"config"`
	}
	data, err = os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &example); err != nil {
		t.Fatal(err)
	}
	var ownership struct {
		ZoneID string `yaml:"zoneId"`
		Zone   string `yaml:"zone"`
	}
	data, err = os.ReadFile("dns-cutover.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &ownership); err != nil {
		t.Fatal(err)
	}
	if ownership.Zone != "is-optimized.com" || ownership.ZoneID == "" || len(example.Config) != 1 || example.Config["is-optimized-edge:zoneId"] != ownership.ZoneID {
		t.Fatal("example does not match the existing zone ownership record")
	}
}
