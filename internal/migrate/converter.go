package migrate

import (
	"fmt"
	"strings"

	"github.com/lzwzzy/binflow/internal/client"
)

// binflowRclasses and binflowPackageTypes mirror the target model
// (internal/repo's supported set). Anything outside needs an explicit
// alias below or is skipped with a recorded reason.
var (
	binflowRclasses     = map[string]bool{"local": true, "remote": true, "virtual": true}
	binflowPackageTypes = map[string]bool{"generic": true, "docker": true, "maven": true, "npm": true, "pypi": true}
)

// artifactoryPackageTypeAliases maps Artifactory package types BinFlow can
// host under a different spelling. Gradle repositories are maven-layout in
// Artifactory, so BinFlow's maven type serves them.
var artifactoryPackageTypeAliases = map[string]string{"gradle": "maven"}

// RepoPlan is the conversion result for one source repository: either a
// ready client.RepoCreateRequest, or a skip with the reason that will be
// shown in the summary and recorded in the progress file.
type RepoPlan struct {
	// Key is the source repository key (BinFlow keeps the same key).
	Key string

	// Request is the write payload when the repository is migratable.
	Request client.RepoCreateRequest

	// Skipped is true when the repository cannot be migrated.
	Skipped bool

	// Reason explains a skip (also shown in the summary).
	Reason string

	// Warnings are non-blocking caveats shown in the summary.
	Warnings []string
}

// ConvertRepo maps an Artifactory repository configuration onto BinFlow's
// create-repository payload.
//
// Mapping rules (kept honest about what cannot be carried):
//
//   - rclass local/remote/virtual map 1:1; federated and unknown classes
//     are skipped (BinFlow has no federation).
//   - packageType generic/docker/maven/npm/pypi map 1:1; gradle aliases to
//     maven (warning); every other Artifactory package type is skipped.
//   - description, includesPattern/excludesPattern (local only — the
//     BinFlow remote/virtual configs do not carry patterns) and the remote
//     upstream url/username are carried verbatim.
//   - The remote upstream PASSWORD is never exportable: Artifactory never
//     echoes credentials. The repo is migrated with a warning; the
//     operator re-enters the password on the target afterwards.
//   - virtual repositories carry their member list; a memberless virtual
//     is skipped (the target rejects it).
func ConvertRepo(src SourceRepoConfig) RepoPlan {
	rclass := strings.ToLower(strings.TrimSpace(src.Rclass))
	pkg := strings.ToLower(strings.TrimSpace(src.PackageType))
	plan := RepoPlan{Key: src.Key}

	switch {
	case strings.TrimSpace(src.Key) == "":
		plan.Skipped = true
		plan.Reason = "repository has no key"
		return plan
	case rclass == "":
		plan.Skipped = true
		plan.Reason = fmt.Sprintf("repository %q has no rclass", src.Key)
		return plan
	case rclass == "federated":
		plan.Skipped = true
		plan.Reason = "federated repositories are not supported by BinFlow"
		return plan
	case !binflowRclasses[rclass]:
		plan.Skipped = true
		plan.Reason = fmt.Sprintf("repository class %q is not supported by BinFlow", rclass)
		return plan
	}

	targetPkg := pkg
	if alias, ok := artifactoryPackageTypeAliases[pkg]; ok {
		targetPkg = alias
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("package type %q maps to BinFlow's %q", pkg, alias))
	} else if !binflowPackageTypes[pkg] {
		plan.Skipped = true
		plan.Reason = fmt.Sprintf("package type %q is not supported by BinFlow", pkg)
		return plan
	}

	plan.Request = client.RepoCreateRequest{
		Key:         src.Key,
		Rclass:      rclass,
		PackageType: targetPkg,
		Description: src.Description,
	}

	switch rclass {
	case "local":
		plan.Request.Includes = src.IncludesPattern
		plan.Request.Excludes = src.ExcludesPattern
	case "remote":
		if strings.TrimSpace(src.URL) == "" {
			plan.Skipped = true
			plan.Reason = "remote repository has no upstream URL"
			return plan
		}
		plan.Request.URL = src.URL
		plan.Request.Username = src.Username
		plan.Warnings = append(plan.Warnings,
			"upstream credentials cannot be exported from Artifactory; re-enter the remote password on the target after migration")
	case "virtual":
		if len(src.Repositories) == 0 {
			plan.Skipped = true
			plan.Reason = "virtual repository has no members"
			return plan
		}
		plan.Request.Members = append([]string(nil), src.Repositories...)
	}
	return plan
}

// UserPlan is the conversion result for one source user.
type UserPlan struct {
	// Name is the (lower-cased) account name; Artifactory lower-cases on
	// create (auth-model.md 1.1) and BinFlow rejects mixed case.
	Name string

	// Email and Admin are carried verbatim; BinFlow requires a non-blank
	// email on create.
	Email string
	Admin bool

	// Groups is the direct group membership, carried verbatim. The groups
	// must already exist on the target (unknown group -> 400).
	Groups []string

	// Skipped is true when the user must not (or cannot) be migrated.
	Skipped bool

	// Reason explains a skip.
	Reason string

	// Warnings are non-blocking caveats.
	Warnings []string
}

// ConvertUser maps an Artifactory user record onto BinFlow's user model.
//
// Skips (each with a recorded reason):
//
//   - anonymous — built into BinFlow, not a migratable account;
//   - _system_ — reserved name the target rejects;
//   - non-internal realms — LDAP/OIDC users are provisioned by their
//     identity provider at login, not by migration (BinFlow M6 provisions
//     them the same way);
//   - missing email — required by the BinFlow create plane.
//
// Passwords are NEVER migratable (Artifactory never echoes them); the
// write phase assigns either the operator-provided shared password or a
// generated per-user one.
func ConvertUser(src SourceUserDetail) UserPlan {
	name := strings.ToLower(strings.TrimSpace(src.Name))
	plan := UserPlan{Name: name}

	switch {
	case name == "":
		plan.Skipped = true
		plan.Reason = "user has no name"
		return plan
	case name == "anonymous":
		plan.Skipped = true
		plan.Reason = "the anonymous account is built into BinFlow"
		return plan
	case name == "_system_":
		plan.Skipped = true
		plan.Reason = "reserved system account"
		return plan
	case src.Realm != "" && src.Realm != "internal":
		plan.Skipped = true
		plan.Reason = fmt.Sprintf("realm %q users are provisioned by their identity provider, not migrated", src.Realm)
		return plan
	case strings.TrimSpace(src.Email) == "":
		plan.Skipped = true
		plan.Reason = "user has no email (required by BinFlow)"
		return plan
	}

	plan.Email = strings.TrimSpace(src.Email)
	plan.Admin = src.Admin
	plan.Groups = append([]string(nil), src.Groups...)
	if len(plan.Groups) > 0 {
		plan.Warnings = append(plan.Warnings,
			"group membership requires the groups to exist on the target (create them first; an unknown group fails this user with 400)")
	}
	return plan
}
