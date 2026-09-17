package httpapi_test

// L025-6 (D02 config four-layer key face, rest-api.md // 2.1.9): the
// exact key-NAME sets of every configuration render face, transcribed
// from the live wire (reports/compatibility/l025q-wire/a/ c01/b01/w06b/v01
// + the L025-6 reference probes on 172.16.58.130:8082, assets l025r-*
// deleted after capture). The dialect key (rclass on configurations/v1/
// batch, type on v2) is appended by the renderer and asserted per face —
// the string constants below carry the OTHER keys, one per line. Any added
// or dropped key regresses these tests; that is their entire job.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

const wantLocalCommon = `
archiveBrowsingEnabled
blackedOut
cdnRedirect
description
downloadRedirect
environments
excludesPattern
includesPattern
key
notes
packageType
priorityResolution
propertySets
repoLayoutRef
signedUrlTtl
xrayDataTtl
xrayIndex`

const wantRemoteCommon = `
allowAnyHostAuth
archiveBrowsingEnabled
assumedOfflinePeriodSecs
blackedOut
blockMismatchingMimeTypes
bypassHeadRequests
contentSynchronisation
curated
customHttpHeaders
description
disableProxy
disableUrlNormalization
downloadRedirect
enableCookieManagement
environments
excludesPattern
hardFail
includesPattern
key
listRemoteFolderItems
localAddress
metadataRetrievalTimeoutSecs
missedRetrievalCachePeriodSecs
notes
offline
packageType
passThrough
password
priorityResolution
propagateQueryParams
propertySets
repoLayoutRef
retrievalCachePeriodSecs
retrieveSha256FromServer
sendContext
shareConfiguration
signedUrlTtl
socketTimeoutMillis
storeArtifactsLocally
synchronizeProperties
unusedArtifactsCleanupPeriodHours
url
username
xrayDataTtl
xrayIndex`

const wantVirtualCommon = `
artifactoryRequestsCanRetrieveRemoteArtifacts
description
environments
excludesPattern
hideUnauthorizedResources
includesPattern
key
notes
packageType
repositories
signedUrlTtl`

const wantLocalFull = `
archiveBrowsingEnabled
blackedOut
blockPushingSchema1
calculateYumMetadata
cargoAnonymousAccess
cargoInternalIndex
cdnRedirect
checksumPolicyType
ddebSupported
debianTrivialLayout
description
dockerApiVersion
dockerProjectId
dockerTagRetention
downloadRedirect
enableBowerSupport
enableChefSupport
enableCocoaPodsSupport
enableComposerSupport
enableComposerV1Indexing
enableConanSupport
enableDebianSupport
enableDistRepoSupport
enableDockerSupport
enableFileListsIndexing
enableGemsSupport
enableGitLfsSupport
enableNormalizedVersion
enableNpmSupport
enableNuGetSupport
enablePuppetSupport
enablePypiSupport
enableVagrantSupport
encryptStates
environments
excludesPattern
forceConanAuthentication
forceMetadataNameVersion
forceNonDuplicateChart
forceNugetAuthentication
forceP2Authentication
handleReleases
handleSnapshots
includesPattern
key
maxUniqueSnapshots
maxUniqueTags
mlRepoLayout
notes
packageType
priorityResolution
propertySets
repoLayoutRef
signedUrlTtl
snapshotVersionBehavior
suppressPomConsistencyChecks
terraformType
xrayDataTtl
xrayIndex
yumRootDepth`

const wantRemoteFull = `
allowAnyHostAuth
archiveBrowsingEnabled
assumedOfflinePeriodSecs
authUrl
blackedOut
blockMismatchingMimeTypes
blockPushingSchema1
bowerRegistryUrl
bypassHeadRequests
cargoAnonymousAccess
cargoInternalIndex
cdnRedirect
composerRegistryUrl
contentSynchronisation
curated
customHttpHeaders
ddebSupported
debianTrivialLayout
description
disableProxy
disableUrlNormalization
dockerApiVersion
dockerProjectId
downloadRedirect
enableBowerSupport
enableChefSupport
enableCocoaPodsSupport
enableComposerSupport
enableConanSupport
enableCookieManagement
enableDebianSupport
enableDistRepoSupport
enableDockerSupport
enableGemsSupport
enableGitLfsSupport
enableNormalizedVersion
enableNpmSupport
enableNuGetSupport
enablePuppetSupport
enablePypiSupport
enableTokenAuthentication
enableVagrantSupport
environments
excludesPattern
externalDependenciesEnabled
fetchJarsEagerly
fetchSourcesEagerly
forceConanAuthentication
forceMetadataNameVersion
forceNonDuplicateChart
forceNugetAuthentication
forceP2Authentication
gitLabResolveSubgroups
gitRegistryUrl
handleReleases
handleSnapshots
hardFail
hexPublicKey
includesPattern
key
listRemoteFolderItems
localAddress
maxUniqueSnapshots
maxUniqueTags
metadataRetrievalTimeoutSecs
missedRetrievalCachePeriodSecs
mlRepoLayout
notes
offline
packageType
passThrough
password
podsCdnUrl
podsForceModifyIndex
podsSpecsRepoUrl
priorityResolution
propagateQueryParams
propertySets
pyPIRegistryUrl
pyPIRepositorySuffix
rejectInvalidJars
remoteRepoChecksumPolicyType
repoLayoutRef
retrievalCachePeriodSecs
retrieveSha256FromServer
sendContext
shareConfiguration
signedUrlTtl
socketTimeoutMillis
storeArtifactsLocally
suppressPomConsistencyChecks
synchronizeProperties
terraformProvidersUrl
terraformRegistryUrl
unusedArtifactsCleanupPeriodHours
url
username
vcsGitProvider
vcsType
xrayDataTtl
xrayIndex`

const wantVirtualFull = `
artifactoryRequestsCanRetrieveRemoteArtifacts
blockPushingSchema1
cachingLocalForeignLayersEnabled
cargoAnonymousAccess
cargoInternalIndex
ddebSupported
debianTrivialLayout
description
dockerApiVersion
dockerProjectId
enableBowerSupport
enableChefSupport
enableCocoaPodsSupport
enableComposerSupport
enableConanSupport
enableDebianSupport
enableDistRepoSupport
enableDockerSupport
enableGemsSupport
enableGitLfsSupport
enableNormalizedVersion
enableNpmSupport
enableNuGetSupport
enablePuppetSupport
enablePypiSupport
enableVagrantSupport
environments
excludesPattern
externalDependenciesEnabled
forceConanAuthentication
forceMavenAuthentication
forceMetadataNameVersion
forceNonDuplicateChart
forceNugetAuthentication
forceP2Authentication
hideUnauthorizedResources
includesPattern
key
keyPair
mlRepoLayout
notes
packageType
pomRepositoryReferencesCleanupPolicy
priorityResolution
repositories
resolveDockerTagsByTimestamp
signedUrlTtl
useNamespaces
virtualRetrievalCachePeriodSecs`

// assertKeySetExact fails unless got is exactly want (set semantics; the
// message names every added and missing key).
func assertKeySetExact(t *testing.T, label string, got map[string]any, wantRaw string) {
	t.Helper()
	want := map[string]bool{}
	for _, k := range strings.Fields(wantRaw) {
		want[k] = true
	}
	added, missing := []string{}, []string{}
	for k := range got {
		if !want[k] {
			added = append(added, k)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(added)
	sort.Strings(missing)
	if len(added) > 0 || len(missing) > 0 {
		t.Errorf("%s: key set drifted (got %d keys): added=%v missing=%v",
			label, len(got), added, missing)
	}
}

// seedProjectionRepos mirrors the wire sample repositories: a local
// generic with an explicit layout, a remote generic with an upstream, and
// a virtual over the local member.
func seedProjectionRepos(t *testing.T, h *harness) {
	t.Helper()
	seedConfigFamilyRepo(t, h, "ploc", `{"rclass":"local","packageType":"generic","repoLayoutRef":"maven-2-default"}`)
	seedConfigFamilyRepo(t, h, "prem", `{"rclass":"remote","packageType":"generic","repoLayoutRef":"maven-2-default","url":"http://upstream.example/artifactory"}`)
	seedConfigFamilyRepo(t, h, "pvirt", `{"rclass":"virtual","packageType":"generic","repositories":["ploc"]}`)
}

// TestRepoConfigKeyFacesAdmin: the admin layers' exact key sets —
// configurations (common projection, rclass), v1 single + v2 batch (the
// full schema, rclass, the same projection on both faces) and v2 single
// (type dialect).
func TestRepoConfigKeyFacesAdmin(t *testing.T) {
	h := newHarness(t)
	seedProjectionRepos(t, h)

	getMap := func(path string) map[string]any {
		resp := h.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, resp.StatusCode, body)
		}
		return decodeJSONMap(t, body)
	}

	tests := []struct {
		label    string
		fetch    func() map[string]any
		wantKeys string
		dialect  string
	}{
		{"v1 local 61", func() map[string]any { return getMap("/binflow/api/repositories/ploc") }, wantLocalFull, "rclass"},
		{"v1 remote 102", func() map[string]any { return getMap("/binflow/api/repositories/prem") }, wantRemoteFull, "rclass"},
		{"v1 virtual 50", func() map[string]any { return getMap("/binflow/api/repositories/pvirt") }, wantVirtualFull, "rclass"},
		{"v2 local 18", func() map[string]any { return getMap("/binflow/api/v2/repositories/ploc") }, wantLocalCommon, "type"},
		{"v2 remote 46", func() map[string]any { return getMap("/binflow/api/v2/repositories/prem") }, wantRemoteCommon, "type"},
		{"v2 virtual 12", func() map[string]any { return getMap("/binflow/api/v2/repositories/pvirt") }, wantVirtualCommon, "type"},
		{"batch local = v1", func() map[string]any {
			m := getMap("/binflow/api/v2/repositories/batch?names=ploc")
			v, _ := m["ploc"].(map[string]any)
			if v == nil {
				t.Fatalf("batch body lacks ploc: %v", m)
			}
			return v
		}, wantLocalFull, "rclass"},
		{"batch remote = v1", func() map[string]any {
			m := getMap("/binflow/api/v2/repositories/batch?names=prem")
			v, _ := m["prem"].(map[string]any)
			if v == nil {
				t.Fatalf("batch body lacks prem: %v", m)
			}
			return v
		}, wantRemoteFull, "rclass"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			m := tt.fetch()
			assertKeySetExact(t, tt.label, m, tt.wantKeys+" "+tt.dialect)
			if m[tt.dialect] == nil {
				t.Errorf("%s: dialect key %q missing", tt.label, tt.dialect)
			}
		})
	}

	t.Run("configurations 18/46/12", func(t *testing.T) {
		m := getMap("/binflow/api/repositories/configurations")
		group := func(name, key, want string) {
			g, _ := m[name].([]any)
			for _, e := range g {
				row, _ := e.(map[string]any)
				if row["key"] == key {
					assertKeySetExact(t, name+" "+key, row, want+" rclass")
					return
				}
			}
			t.Fatalf("%s lacks %s: %v", name, key, m[name])
		}
		group("LOCAL", "ploc", wantLocalCommon)
		group("REMOTE", "prem", wantRemoteCommon)
		group("VIRTUAL", "pvirt", wantVirtualCommon)
	})

	// Local rows never carry url on any admin face (the L025-4 flagged
	// drift: BinFlow used to fabricate a context URL).
	t.Run("local carries no url on any face", func(t *testing.T) {
		for _, path := range []string{
			"/binflow/api/repositories/ploc",
			"/binflow/api/v2/repositories/ploc",
		} {
			if _, has := getMap(path)["url"]; has {
				t.Errorf("%s: local row carries url", path)
			}
		}
	})
}

// TestRepoConfigKeyFacesNonAdmin: the partial projection is PER-RCLASS —
// key/packageType/description plus the dialect key, url only on remote,
// repositories only on virtual, nothing else (the L025-6 probe; the
// pre-fix code fabricated a context url on local).
func TestRepoConfigKeyFacesNonAdmin(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedConfigFamilyRepo(t, h, "ploc", `{"rclass":"local","packageType":"generic"}`)
	seedConfigFamilyRepo(t, h, "prem", `{"rclass":"remote","packageType":"generic","url":"http://upstream.example/artifactory"}`)
	seedConfigFamilyRepo(t, h, "pvirt", `{"rclass":"virtual","packageType":"generic","repositories":["ploc"]}`)

	getMap := func(path string) map[string]any {
		resp := h.do(http.MethodGet, path, "u1", "p1", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, resp.StatusCode, body)
		}
		return decodeJSONMap(t, body)
	}

	t.Run("v2 partial per rclass", func(t *testing.T) {
		for _, tt := range []struct {
			label, path, extra string
		}{
			{"local four keys", "/binflow/api/v2/repositories/ploc", "type"},
			{"remote five keys", "/binflow/api/v2/repositories/prem", "type url"},
			{"virtual five keys", "/binflow/api/v2/repositories/pvirt", "type repositories"},
		} {
			t.Run(tt.label, func(t *testing.T) {
				assertKeySetExact(t, tt.label, getMap(tt.path), "description key packageType "+tt.extra)
			})
		}
	})

	t.Run("batch partial carries the rclass dialect", func(t *testing.T) {
		m := getMap("/binflow/api/v2/repositories/batch?names=ploc&names=prem")
		v, _ := m["ploc"].(map[string]any)
		if v == nil {
			t.Fatalf("batch body lacks ploc: %v", m)
		}
		assertKeySetExact(t, "batch local", v, "description key packageType rclass")
		rv, _ := m["prem"].(map[string]any)
		if rv == nil {
			t.Fatalf("batch body lacks prem: %v", m)
		}
		assertKeySetExact(t, "batch remote", rv, "description key packageType rclass url")
	})

	// L026-7 (the L026-5 spec ruling): the v1 face serves every
	// authenticated user the same per-rclass partial projection the v2
	// face does — the family-7 route gate is gone, coverage plays no
	// part (wire a-holder-v1-*/a-noperm-v1-*).
	t.Run("v1 plain user same per-rclass partial", func(t *testing.T) {
		for _, tt := range []struct {
			label, path, extra string
		}{
			{"local four keys", "/binflow/api/repositories/ploc", "rclass"},
			{"remote five keys", "/binflow/api/repositories/prem", "rclass url"},
			{"virtual five keys", "/binflow/api/repositories/pvirt", "rclass repositories"},
		} {
			t.Run(tt.label, func(t *testing.T) {
				assertKeySetExact(t, tt.label, getMap(tt.path), "description key packageType "+tt.extra)
			})
		}
	})
}

// TestRepoConfigFaceValueShapes: the unmodeled keys render the wire's
// measured default shapes, stored fields ride the top level, and the flat
// contentSynchronisation is reshaped to the nested wire form.
func TestRepoConfigFaceValueShapes(t *testing.T) {
	h := newHarness(t)
	seedProjectionRepos(t, h)

	v1 := func(key string) map[string]any {
		resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", key, resp.StatusCode)
		}
		return decodeJSONMap(t, mustGet(t, resp))
	}
	loc, rem, virt := v1("ploc"), v1("prem"), v1("pvirt")

	for k, want := range map[string]any{
		"notes": "", "signedUrlTtl": 90, "blockPushingSchema1": true,
		"dockerApiVersion": "V2", "terraformType": "MODULE", "encryptStates": true,
		"dockerTagRetention": 1, "snapshotVersionBehavior": "unique",
		"checksumPolicyType": "client-checksums", "maxUniqueTags": 0,
		"propertySets": []any{}, "environments": []any{},
	} {
		if fmt.Sprint(loc[k]) != fmt.Sprint(want) {
			t.Errorf("local %s = %v (%T), want %v", k, loc[k], loc[k], want)
		}
	}
	if rem["url"] != "http://upstream.example/artifactory" || rem["password"] != "" {
		t.Errorf("remote url/password = %v/%v", rem["url"], rem["password"])
	}
	if fmt.Sprint(virt["repositories"]) != "[ploc]" {
		t.Errorf("virtual repositories = %v", virt["repositories"])
	}
	if virt["cargoInternalIndex"] != true || loc["cargoInternalIndex"] != false {
		t.Errorf("cargoInternalIndex virtual=%v local=%v (wire: true/false)",
			virt["cargoInternalIndex"], loc["cargoInternalIndex"])
	}
	cs, ok := rem["contentSynchronisation"].(map[string]any)
	if !ok {
		t.Fatalf("contentSynchronisation = %v", rem["contentSynchronisation"])
	}
	wantCS := map[string]any{
		"enabled":    false,
		"statistics": map[string]any{"enabled": false},
		"properties": map[string]any{"enabled": false},
		"source":     map[string]any{"originAbsenceDetection": false},
	}
	if fmt.Sprint(cs) != fmt.Sprint(wantCS) {
		t.Errorf("contentSynchronisation = %v, want %v", cs, wantCS)
	}
	seedConfigFamilyRepo(t, h, "pcs",
		`{"rclass":"remote","packageType":"generic","url":"http://upstream.example/x","contentSynchronisation":{"statisticsEnabled":true}}`)
	cs, _ = v1("pcs")["contentSynchronisation"].(map[string]any)
	stats, _ := cs["statistics"].(map[string]any)
	if cs["enabled"] != false || stats["enabled"] != true {
		t.Errorf("nested reshape of a flat knob = %v", cs)
	}

	if _, has := virt["repoLayoutRef"]; has {
		t.Errorf("unset virtual repoLayoutRef must be omitted: %v", virt["repoLayoutRef"])
	}
	seedConfigFamilyRepo(t, h, "pv2", `{"rclass":"virtual","packageType":"generic","repoLayoutRef":"simple-default","repositories":["ploc"]}`)
	if v1("pv2")["repoLayoutRef"] != "simple-default" {
		t.Errorf("set virtual repoLayoutRef = %v, want simple-default", v1("pv2")["repoLayoutRef"])
	}

	seedConfigFamilyRepo(t, h, "pknob", `{"rclass":"local","packageType":"generic","blackedOut":true,"environments":["DEV"],"handleReleases":false}`)
	knob := v1("pknob")
	if knob["blackedOut"] != true || knob["handleReleases"] != false || fmt.Sprint(knob["environments"]) != "[DEV]" {
		t.Errorf("stored knobs = %v %v %v", knob["blackedOut"], knob["handleReleases"], knob["environments"])
	}
	seedConfigFamilyRepo(t, h, "pstg", `{"rclass":"local","packageType":"generic","stages":["BOX"]}`)
	if fmt.Sprint(v1("pstg")["environments"]) != "[BOX]" {
		t.Errorf("stages alias = %v, want environments [BOX]", v1("pstg")["environments"])
	}
}

// TestRepoConfigUnmodeledKeyEcho (L026-3): stored keys the seat tables do
// not claim ride the admin configuration faces verbatim — the BinFlow-native
// policy family (the deb/rpm index-engine keys, quotaBytes) round-trips
// PUT→GET on the top level of the measured flat body, while the measured
// key-NAME sets stay exact for repositories that never carried them (the
// echo fires only on a stored key; the deb keys ride a generic repository
// because the transport stores the family package-type-agnostically). The
// stages alias never leaks under its own spelling and the non-admin
// partial projection stays narrow.
func TestRepoConfigUnmodeledKeyEcho(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "pecho", `{"rclass":"local","packageType":"generic",`+
		`"byHash":"SHA256","origin":"o","label":"l","historyCycles":5,`+
		`"optionalIndexCompressionFormats":["xz"],"debianDefaultArchitectures":"amd64,s390x",`+
		`"yumGroupFileNames":"comps.xml","quotaBytes":123}`)

	getMap := func(path string) map[string]any {
		resp := h.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, resp.StatusCode, body)
		}
		return decodeJSONMap(t, body)
	}
	echoExtras := "byHash origin label historyCycles optionalIndexCompressionFormats " +
		"debianDefaultArchitectures yumGroupFileNames quotaBytes"

	v1 := getMap("/binflow/api/repositories/pecho")
	for k, want := range map[string]any{
		"byHash":                     "SHA256",
		"origin":                     "o",
		"label":                      "l",
		"historyCycles":              float64(5),
		"debianDefaultArchitectures": "amd64,s390x",
		"yumGroupFileNames":          "comps.xml",
		"quotaBytes":                 float64(123),
	} {
		if v1[k] != want {
			t.Errorf("v1[%s] = %v, want %v", k, v1[k], want)
		}
	}
	if fmt.Sprint(v1["optionalIndexCompressionFormats"]) != "[xz]" {
		t.Errorf("v1[optionalIndexCompressionFormats] = %v, want [xz]", v1["optionalIndexCompressionFormats"])
	}
	// The echo adds EXACTLY the stored extras — the measured 61-key face
	// plus the nine keys above, nothing else.
	assertKeySetExact(t, "v1 echo face", v1, wantLocalFull+" rclass "+echoExtras)

	// The shared renderer carries the echo onto the v2 and configurations
	// faces (the same stored blob), each still its measured set + extras.
	v2 := getMap("/binflow/api/v2/repositories/pecho")
	if v2["byHash"] != "SHA256" {
		t.Errorf("v2[byHash] = %v, want SHA256 (shared renderer)", v2["byHash"])
	}
	assertKeySetExact(t, "v2 echo face", v2, wantLocalCommon+" type "+echoExtras)
	cfg := getMap("/binflow/api/repositories/configurations")
	for _, e := range cfg["LOCAL"].([]any) {
		row := e.(map[string]any)
		if row["key"] == "pecho" {
			assertKeySetExact(t, "configurations echo face", row, wantLocalCommon+" rclass "+echoExtras)
		}
	}

	// The stages alias stays a storage spelling: its value rides the
	// environments seat, never a top-level stages key.
	seedConfigFamilyRepo(t, h, "pstg2", `{"rclass":"local","packageType":"generic","stages":["BOX"]}`)
	if _, has := getMap("/binflow/api/repositories/pstg2")["stages"]; has {
		t.Error("stages alias leaked onto the face under its own spelling")
	}

	// The non-admin partial projection stays the measured four keys — the
	// echo widens no unauthenticated information face.
	h2 := newHarnessCfg(t, nil, [][2]string{{"u2", "p2"}})
	seedConfigFamilyRepo(t, h2, "pecho", `{"rclass":"local","packageType":"generic","byHash":"SHA256"}`)
	resp := h2.do(http.MethodGet, "/binflow/api/v2/repositories/pecho", "u2", "p2", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("non-admin v2 GET = %d", resp.StatusCode)
	}
	assertKeySetExact(t, "non-admin face", decodeJSONMap(t, mustGet(t, resp)), "description key packageType type")
}
