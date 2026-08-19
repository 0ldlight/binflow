package maven

import "testing"

// TestParseLayoutMatrix is the T-67 AC③ layout table (28 cases): the
// classifier order (checksum suffix -> metadata names -> artifact
// template), the unique-snapshot grammar, the plugin-group metadata
// variant and the M20 negatives. The raw dot-segment defense itself lives
// in adapter.Layout and is exercised by the handler tests.
func TestParseLayoutMatrix(t *testing.T) {
	cases := []struct {
		name string
		path string
		want Layout
		err  bool
	}{
		// ---- artifacts: release ----
		{
			name: "release jar, dotted group",
			path: "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme", Module: "demo-app", VersionDir: "1.0.0", BaseRev: "1.0.0", File: "demo-app-1.0.0.jar"},
		},
		{
			name: "release pom, real commons-lang3 GAV",
			path: "org/apache/commons/commons-lang3/3.12.0/commons-lang3-3.12.0.pom",
			want: Layout{Kind: KindArtifact, OrgPath: "org.apache.commons", Module: "commons-lang3", VersionDir: "3.12.0", BaseRev: "3.12.0"},
		},
		{
			name: "group path that stops one segment early does not match",
			path: "org/apache/commons/lang3/3.12.0/commons-lang3-3.12.0.pom",
			err:  true, // module=lang3, but the file must start "lang3-…"
		},
		{
			name: "classifier jar",
			path: "com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme", Module: "demo-app", VersionDir: "1.0.0", File: "demo-app-1.0.0-sources.jar"},
		},
		{
			name: "classifier with dot and deep group",
			path: "com/acme/internal/team/demo-lib/2.0/demo-lib-2.0-bin.tar.gz",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme.internal.team", Module: "demo-lib", VersionDir: "2.0", BaseRev: "2.0"},
		},
		{
			name: "extension longer than the dir spelling (ext is arbitrary)",
			path: "com/acme/demo/1.0/demo-1.0.0.jar",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme", Module: "demo", VersionDir: "1.0", BaseRev: "1.0"},
		},
		// ---- artifacts: snapshot ----
		{
			name: "non-unique snapshot jar",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-SNAPSHOT.jar",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme", Module: "demo-app", VersionDir: "1.2.0-SNAPSHOT", BaseRev: "1.2.0", Snapshot: true, File: "demo-app-1.2.0-SNAPSHOT.jar"},
		},
		{
			name: "non-unique snapshot pom with classifier",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-SNAPSHOT-tests.jar",
			want: Layout{Kind: KindArtifact, Module: "demo-app", VersionDir: "1.2.0-SNAPSHOT", BaseRev: "1.2.0", Snapshot: true},
		},
		{
			name: "unique timestamped snapshot",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20240819.101500-1.jar",
			want: Layout{Kind: KindArtifact, OrgPath: "com.acme", Module: "demo-app", VersionDir: "1.2.0-SNAPSHOT", BaseRev: "1.2.0", Snapshot: true, Timestamped: true, File: "demo-app-1.2.0-20240819.101500-1.jar"},
		},
		{
			name: "unique timestamped snapshot, buildNumber 10",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20240819.101500-10.pom",
			want: Layout{Kind: KindArtifact, Module: "demo-app", BaseRev: "1.2.0", Snapshot: true, Timestamped: true},
		},
		{
			name: "unique timestamped snapshot with classifier",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20240819.101500-2-tests.jar",
			want: Layout{Kind: KindArtifact, Module: "demo-app", BaseRev: "1.2.0", Snapshot: true, Timestamped: true},
		},
		{
			name: "timestamped with wrong time width",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-2024819.101500-1.jar",
			err:  true,
		},
		{
			name: "snapshot dir but release-spelled file",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0.jar",
			err:  true, // in a -SNAPSHOT dir the file must spell -SNAPSHOT or the timestamped form
		},
		// ---- metadata ----
		{
			name: "module-level metadata",
			path: "com/acme/demo-app/maven-metadata.xml",
			want: Layout{Kind: KindMetadata, OrgPath: "com.acme", Module: "demo-app", File: "maven-metadata.xml"},
		},
		{
			name: "version-level metadata (module-level reading of the last dir)",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml",
			want: Layout{Kind: KindMetadata, OrgPath: "com.acme.demo-app", Module: "1.2.0-SNAPSHOT", File: "maven-metadata.xml"},
		},
		{
			name: "plugin-group metadata (org.apache.maven.plugins)",
			path: "org/apache/maven/plugins/maven-metadata.xml",
			want: Layout{Kind: KindMetadata, OrgPath: "org.apache.maven", Module: "plugins"},
		},
		{
			name: "plugin-group metadata variant spelling",
			path: "org/apache/maven/plugins/metadata-maven-metadata.xml",
			want: Layout{Kind: KindMetadata, OrgPath: "org.apache.maven", Module: "plugins", File: "metadata-maven-metadata.xml"},
		},
		{
			name: "metadata directly under repo root",
			path: "maven-metadata.xml",
			err:  true,
		},
		{
			name: "metadata with only a group dir",
			path: "com/maven-metadata.xml",
			err:  true,
		},
		// ---- sidecars ----
		{
			name: "sha1 sidecar of release jar",
			path: "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar.sha1",
			want: Layout{Kind: KindSidecar, Target: "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", TargetKind: KindArtifact, Algo: "sha1", File: "demo-app-1.0.0.jar.sha1"},
		},
		{
			name: "md5 sidecar of pom",
			path: "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom.md5",
			want: Layout{Kind: KindSidecar, Target: "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", TargetKind: KindArtifact, Algo: "md5"},
		},
		{
			name: "sha256 sidecar of timestamped snapshot",
			path: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20240819.101500-1.jar.sha256",
			want: Layout{Kind: KindSidecar, Target: "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20240819.101500-1.jar", TargetKind: KindArtifact, Algo: "sha256", Snapshot: true, Timestamped: true},
		},
		{
			name: "sha1 sidecar of metadata",
			path: "com/acme/demo-app/maven-metadata.xml.sha1",
			want: Layout{Kind: KindSidecar, Target: "com/acme/demo-app/maven-metadata.xml", TargetKind: KindMetadata, Algo: "sha1"},
		},
		{
			name: "sha512 sidecar recognized for layout",
			path: "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar.sha512",
			want: Layout{Kind: KindSidecar, Target: "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", TargetKind: KindArtifact, Algo: "sha512"},
		},
		{
			name: "sidecar of metadata variant spelling",
			path: "org/apache/maven/plugins/metadata-maven-metadata.xml.sha1",
			want: Layout{Kind: KindSidecar, Target: "org/apache/maven/plugins/metadata-maven-metadata.xml", TargetKind: KindMetadata, Algo: "sha1"},
		},
		// ---- negatives (M20 family + structure) ----
		{name: "no group segment", path: "demo-app/1.0.0/demo-app-1.0.0.jar", err: true},
		{name: "bare file (M20 case 1)", path: "foo.jar", err: true},
		{name: "file name not module-version (M20 case 2)", path: "com/acme/demo/1.0.0/zzz-1.0.0.jar", err: true},
		{name: "file name without extension", path: "com/acme/demo/1.0.0/demo-1.0.0", err: true},
		{name: "file name ends with dot", path: "com/acme/demo/1.0.0/demo-1.0.0.", err: true},
		{name: "classifier without extension", path: "com/acme/demo/1.0.0/demo-1.0.0-sources", err: true},
		{name: "version dir bare -SNAPSHOT", path: "com/acme/demo/-SNAPSHOT/demo--SNAPSHOT.jar", err: true},
		{name: "bare checksum suffix file", path: "com/acme/demo/1.0.0/.sha1", err: true},
		{name: "sidecar of nothing", path: "com/acme/foo.jar.sha1", err: true},
		{name: "checksum suffix on a group dir", path: "com/acme.sha1", err: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.path)
			if tc.err {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want error", tc.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.path, err)
			}
			if got.Kind != tc.want.Kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.want.Kind)
			}
			if tc.want.OrgPath != "" && got.OrgPath != tc.want.OrgPath {
				t.Errorf("OrgPath = %q, want %q", got.OrgPath, tc.want.OrgPath)
			}
			if tc.want.Module != "" && got.Module != tc.want.Module {
				t.Errorf("Module = %q, want %q", got.Module, tc.want.Module)
			}
			if tc.want.VersionDir != "" && got.VersionDir != tc.want.VersionDir {
				t.Errorf("VersionDir = %q, want %q", got.VersionDir, tc.want.VersionDir)
			}
			if tc.want.BaseRev != "" && got.BaseRev != tc.want.BaseRev {
				t.Errorf("BaseRev = %q, want %q", got.BaseRev, tc.want.BaseRev)
			}
			if got.Snapshot != tc.want.Snapshot {
				t.Errorf("Snapshot = %v, want %v", got.Snapshot, tc.want.Snapshot)
			}
			if got.Timestamped != tc.want.Timestamped {
				t.Errorf("Timestamped = %v, want %v", got.Timestamped, tc.want.Timestamped)
			}
			if tc.want.File != "" && got.File != tc.want.File {
				t.Errorf("File = %q, want %q", got.File, tc.want.File)
			}
			if tc.want.Target != "" && got.Target != tc.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tc.want.Target)
			}
			if tc.want.TargetKind != "" && got.TargetKind != tc.want.TargetKind {
				t.Errorf("TargetKind = %q, want %q", got.TargetKind, tc.want.TargetKind)
			}
			if tc.want.Algo != "" && got.Algo != tc.want.Algo {
				t.Errorf("Algo = %q, want %q", got.Algo, tc.want.Algo)
			}
		})
	}
}

// TestGAV pins the GAV rendering the metadata provider consumes.
func TestGAV(t *testing.T) {
	if gav := (Layout{OrgPath: "com.acme", Module: "demo-app"}).GAV(); gav != "com.acme:demo-app" {
		t.Fatalf("GAV() = %q", gav)
	}
}
