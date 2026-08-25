package nuget

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestClientEndToEnd drives the REAL dotnet toolchain against a real
// assembled BinFlow stack — FR-88's hard requirement (a protocol ticket
// is not done on HTTP-layer tests alone). Legs (the L14/L15 command
// matrix):
//
//	L14 local push: dotnet pack a real project, `dotnet nuget push`
//	    onto the v3 index of a local repository (Basic credentials via
//	    nuget.config), then REST-reconcile the landed storage;
//	L15 local consume: `dotnet add package --source` + `dotnet restore`
//	    + `dotnet build` in a scratch console project that REFERENCES the
//	    pushed package (the restore walks index.json -> registration ->
//	    flatcontainer and verifies the packageHash on the way in);
//	L15' remote consume: the same restore through a REMOTE repository
//	    whose upstream is a loopback fake speaking the nuget.org v3
//	    dialect but serving the REAL dotnet-packed bytes and their REAL
//	    sha512 (pull-through with the whole-flow one-contact cache
//	    assertion) — hermetic while crossing the engine's full chain;
//	    the public-network nuget.org form lives in the web/e2e spec.
//
// It is environment-gated: set BINFLOW_T287_CLIENT_E2E=1 with a dotnet
// SDK on PATH (DOTNET_ROOT honored). The gate skips silently when the
// env is unset so CI stays green; the ticket log records a full run.
func TestClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T287_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T287_CLIENT_E2E=1 (with a dotnet SDK on PATH) to run the real-client matrix")
	}
	dotnet := requireDotnet(t)

	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)

	// ---- L14: pack + push ----
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "T287.Lib.csproj"), `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <PackageId>T287.Lib</PackageId>
    <Version>1.0.0</Version>
    <Authors>BinFlow Test</Authors>
    <Description>The T-287 real-client fixture package.</Description>
    <PackageOutputPath>`+filepath.ToSlash(src)+`</PackageOutputPath>
  </PropertyGroup>
</Project>
`)
	writeFile(t, filepath.Join(src, "Greeter.cs"), `namespace T287.Lib;

public static class Greeter
{
    public static string Hello() => "hello from T287.Lib";
}
`)
	runCmd(t, src, dotnet, "pack", "--nologo", "-v", "q")
	nupkg := filepath.Join(src, "T287.Lib.1.0.0.nupkg")
	if _, err := os.Stat(nupkg); err != nil {
		t.Fatalf("packed nupkg missing: %v", err)
	}

	// nuget.config with credentials (push needs Basic; the write plane
	// demands a credential) and the BinFlow source.
	pushDir := t.TempDir()
	writeFile(t, filepath.Join(pushDir, "nuget.config"), fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow" value="%s/index.json" />
  </packageSources>
  <packageSourceCredentials>
    <binflow>
      <add key="Username" value="%s" />
      <add key="ClearTextPassword" value="%s" />
    </binflow>
  </packageSourceCredentials>
</configuration>
`, s.srv.URL+"/binflow/api/nuget/v3/ng-local", adminUser, adminPass))
	out := runCmd(t, pushDir, dotnet, "nuget", "push", nupkg, "--source", "binflow", "--api-key", "ignored", "--skip-duplicate")
	t.Logf("L14 dotnet nuget push: %s", oneLine(out))

	// REST reconciliation: the storage landed in the flatcontainer shape.
	status, body, _ := s.get(apiPath("ng-local") + "/flatcontainer/t287.lib/index.json")
	if status != 200 || !strings.Contains(body, `"1.0.0"`) {
		t.Fatalf("L14 landed versions = (%d, %s)", status, body)
	}
	status, _, _ = s.get(packagePath("ng-local", "t287.lib", "1.0.0", "nupkg"))
	if status != 200 {
		t.Fatalf("L14 landed nupkg GET = %d", status)
	}

	// ---- L15: consume from the local repository ----
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "nuget.config"), fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow" value="%s/index.json" />
  </packageSources>
</configuration>
`, s.srv.URL+"/binflow/api/nuget/v3/ng-local"))
	writeFile(t, filepath.Join(app, "App.csproj"), `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
</Project>
`)
	writeFile(t, filepath.Join(app, "Program.cs"), `Console.WriteLine(T287.Lib.Greeter.Hello());
`)
	// dotnet add package consults the SEARCH service for the best version.
	out = runCmd(t, app, dotnet, "add", "package", "T287.Lib", "--source", s.srv.URL+"/binflow/api/nuget/v3/ng-local/index.json")
	t.Logf("L15 dotnet add package: %s", oneLine(out))
	out = runCmd(t, app, dotnet, "restore", "--nologo", "-v", "q")
	t.Logf("L15 dotnet restore: %s", oneLine(out))
	out = runCmd(t, app, dotnet, "run", "--nologo", "-v", "q")
	if got := oneLine(out); got != "hello from T287.Lib" {
		t.Fatalf("L15 dotnet run output = %q, want the package's greeting", got)
	}
	t.Logf("L15 dotnet run: %s", oneLine(out))

	// ---- L15': the remote pull-through leg ----
	// The upstream is a loopback fake speaking the nuget.org v3 dialect
	// (the provider's upstream prefixes) but serving the REAL dotnet
	// package bytes and their REAL sha512 — the client verifies the hash
	// exactly as it would against nuget.org, so the fixture cannot lie.
	// Hermetic by construction; the public-network form lives in the
	// web/e2e spec against the VM instance.
	up2 := t.TempDir()
	writeFile(t, filepath.Join(up2, "T287.Dep.csproj"), `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <PackageId>T287.Dep</PackageId>
    <Version>2.0.0</Version>
    <Authors>BinFlow Test</Authors>
    <Description>The T-287 remote-leg fixture package.</Description>
    <PackageOutputPath>`+filepath.ToSlash(up2)+`</PackageOutputPath>
  </PropertyGroup>
</Project>
`)
	writeFile(t, filepath.Join(up2, "Dep.cs"), `namespace T287.Dep;

public static class Dep
{
    public static string Name() => "T287.Dep";
}
`)
	runCmd(t, up2, dotnet, "pack", "--nologo", "-v", "q")
	depPkg := filepath.Join(up2, "T287.Dep.2.0.0.nupkg")
	body2, err := os.ReadFile(depPkg)
	if err != nil {
		t.Fatalf("read packed dep: %v", err)
	}
	up := newFakeUpstream(t)
	up.serve(t, "/"+upstreamRegistrationPrefix+"/t287.dep/index.json",
		upstreamRegistration("t287.dep", "T287.Dep", "2.0.0", up.srv.URL,
			&nupkgFixture{body: body2, sha512: sha512Base64Of(t, body2)}), "application/json")
	up.serve(t, "/v3-flatcontainer/t287.dep/index.json",
		[]byte(`{"versions":["2.0.0"]}`), "application/json")
	up.serve(t, "/"+remNupkgPath("t287.dep", "2.0.0"), body2, "application/octet-stream")
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	// Warm the cache with one proxied nupkg read (also feeds the search
	// facts walk): the client's restore then rides the cached copy.
	if status, resp, _ := s.get(apiPath("ng-remote") + "/flatcontainer/t287.dep/2.0.0/t287.dep.2.0.0.nupkg"); status != 200 {
		t.Fatalf("remote warm-up nupkg GET = %d %s", status, resp)
	}
	if n := up.count("/" + remNupkgPath("t287.dep", "2.0.0")); n != 1 {
		t.Fatalf("warm-up upstream contacts = %d, want 1", n)
	}

	app2 := t.TempDir()
	writeFile(t, filepath.Join(app2, "nuget.config"), fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow-remote" value="%s/index.json" />
  </packageSources>
</configuration>
`, s.srv.URL+"/binflow/api/nuget/v3/ng-remote"))
	writeFile(t, filepath.Join(app2, "App.csproj"), `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
</Project>
`)
	writeFile(t, filepath.Join(app2, "Program.cs"), `Console.WriteLine(T287.Dep.Dep.Name());
`)
	out = runCmd(t, app2, dotnet, "add", "package", "T287.Dep", "--version", "2.0.0", "--source", s.srv.URL+"/binflow/api/nuget/v3/ng-remote/index.json")
	t.Logf("L15' dotnet add package (remote): %s", oneLine(out))
	out = runCmd(t, app2, dotnet, "restore", "--nologo", "-v", "q")
	t.Logf("L15' dotnet restore (remote): %s", oneLine(out))
	out = runCmd(t, app2, dotnet, "run", "--nologo", "-v", "q")
	if got := oneLine(out); got != "T287.Dep" {
		t.Fatalf("L15' dotnet run output = %q, want the remote package's answer", got)
	}
	t.Logf("L15' dotnet run: %s", oneLine(out))

	// The cache assertion: the whole client flow (add + restore + run)
	// must not have re-contacted the upstream — the warm-up's landing is
	// the copy every later read serves.
	if n := up.count("/" + remNupkgPath("t287.dep", "2.0.0")); n != 1 {
		t.Errorf("L15' upstream nupkg contacts = %d, want 1 (the client flow must ride the cache)", n)
	} else {
		t.Log("L15' upstream contacted exactly once across the whole client flow")
	}
}

// sha512Base64Of renders one body's official digest spelling.
func sha512Base64Of(t *testing.T, body []byte) string {
	t.Helper()
	sum := sha512.Sum512(body)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// TestClientLiveUpstream restores a REAL package from the REAL public
// upstream through a BinFlow remote repository — the strongest form of the
// pull-through leg (forced gzip on the registration endpoints, the
// mirror-redirect hop, byte-range real documents). Environment-gated
// twice over: the opt-in plus an upstream/package/version triple, because
// the reachable mirror set varies by network (this ticket's log records
// the nuget.azure.cn staleness probe — pick a version whose artifacts the
// network actually serves).
func TestClientLiveUpstream(t *testing.T) {
	if os.Getenv("BINFLOW_T287_CLIENT_E2E") != "1" || os.Getenv("BINFLOW_T287_LIVE_UPSTREAM") == "" {
		t.Skip("set BINFLOW_T287_CLIENT_E2E=1 plus BINFLOW_T287_LIVE_UPSTREAM/_LIVE_ID/_LIVE_VERSION for the public-upstream leg")
	}
	dotnet := requireDotnet(t)
	upstream := os.Getenv("BINFLOW_T287_LIVE_UPSTREAM")
	pkgID := envOr("BINFLOW_T287_LIVE_ID", "newtonsoft.json")
	pkgVersion := envOr("BINFLOW_T287_LIVE_VERSION", "13.0.2")
	displayID := envOr("BINFLOW_T287_LIVE_DISPLAY_ID", "Newtonsoft.Json")

	s := newStack(t)
	s.seedRepo(t, "ng-live", repo.TypeRemote)
	s.seedRemoteConfig(t, "ng-live", upstream)

	app := t.TempDir()
	writeFile(t, filepath.Join(app, "nuget.config"), fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow-live" value="%s/index.json" />
  </packageSources>
</configuration>
`, s.srv.URL+"/binflow/api/nuget/v3/ng-live"))
	writeFile(t, filepath.Join(app, "App.csproj"), `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
</Project>
`)
	writeFile(t, filepath.Join(app, "Program.cs"), `Console.WriteLine(Newtonsoft.Json.JsonConvert.SerializeObject(new { ok = true, from = "binflow" }));
`)
	out := runCmd(t, app, dotnet, "add", "package", displayID, "--version", pkgVersion, "--source", s.srv.URL+"/binflow/api/nuget/v3/ng-live/index.json")
	t.Logf("LIVE dotnet add package: %s", oneLine(out))
	out = runCmd(t, app, dotnet, "restore", "--nologo", "-v", "q")
	t.Logf("LIVE dotnet restore: %s", oneLine(out))
	out = runCmd(t, app, dotnet, "run", "--nologo", "-v", "q")
	if got := oneLine(out); !strings.Contains(got, `"from":"binflow"`) {
		t.Fatalf("LIVE dotnet run output = %q, want the Newtonsoft-serialized greeting", got)
	}
	t.Logf("LIVE dotnet run: %s", oneLine(out))
	_ = pkgID
}

// envOr reads one env var with a default.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// requireDotnet resolves the dotnet CLI (DOTNET_ROOT honored — the
// tarball install of the ticket log).
func requireDotnet(t *testing.T) string {
	t.Helper()
	candidates := []string{"dotnet"}
	if root := os.Getenv("DOTNET_ROOT"); root != "" {
		candidates = append([]string{filepath.Join(root, "dotnet")}, candidates...)
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	t.Fatalf("dotnet SDK unavailable on PATH (set DOTNET_ROOT; see the ticket runbook)")
	return ""
}

// runCmd runs one dotnet command in dir, failing the test on a non-zero
// exit (exit codes are the asserted object).
func runCmd(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Deterministic, hermetic restores: no telemetry, no first-run
	// experience, no implicit nuget.org fallback (a clear packageSources
	// in nuget.config does the source half; these flags do the rest).
	home, err := os.MkdirTemp("", "t287-dotnet-home")
	if err != nil {
		t.Fatalf("temp home: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(home, 0o755)
		_ = os.RemoveAll(home) //nolint:errcheck // best-effort temp cleanup
	})
	cmd.Env = append(os.Environ(),
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"DOTNET_MULTILEVEL_LOOKUP=0",
		"HOME="+home,
		"USER="+os.Getenv("USER"),
		"NUGET_XMLDOC_MODE=skip",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// writeFile writes one fixture file (parent must exist).
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// oneLine squeezes a command output to its last non-empty line (the run
// legs' console answer).
func oneLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
