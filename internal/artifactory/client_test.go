package artifactory

import "testing"

func TestSplitPath(t *testing.T) {
	cases := []struct {
		path        string
		wantGroup   string
		wantVersion string
	}{
		{"my-image/latest", "my-image", "latest"},
		{"team/my-image/sha256:abc", "team/my-image", "sha256:abc"},
		{"a/b/c/d", "a/b/c", "d"},
		{"sha256:abc", "", "sha256:abc"},
		{"", "", ""},
		{"/version", "", "/version"},
	}

	for _, c := range cases {
		group, version := splitPath(c.path)
		if group != c.wantGroup || version != c.wantVersion {
			t.Errorf("splitPath(%q) = (%q, %q), want (%q, %q)",
				c.path, group, version, c.wantGroup, c.wantVersion)
		}
	}
}

func TestBuildAQL_ExactName(t *testing.T) {
	q := buildAQL(ArtifactFilter{Repo: "docker-local", Name: "list.manifest.json"})
	want := `items.find({"repo":"docker-local","name":"list.manifest.json"})`
	if q != want {
		t.Errorf("got  %q\nwant %q", q, want)
	}
}

func TestBuildAQL_GlobName(t *testing.T) {
	q := buildAQL(ArtifactFilter{Repo: "libs", Name: "*.pom", PathMatch: "*example*"})
	want := `items.find({"repo":"libs","name":{"$match":"*.pom"},"path":{"$match":"*example*"}})`
	if q != want {
		t.Errorf("got  %q\nwant %q", q, want)
	}
}

func TestBuildAQL_PathNoMatch(t *testing.T) {
	q := buildAQL(ArtifactFilter{Repo: "docker", Name: "manifest.json", PathNoMatch: "*/sha256:*"})
	want := `items.find({"repo":"docker","name":"manifest.json","path":{"$nmatch":"*/sha256:*"}})`
	if q != want {
		t.Errorf("got  %q\nwant %q", q, want)
	}
}