package httpapi

// L025-3A (D02 layout face, rest-api.md section 2.1.8): the repository
// layout read plane. The retired official mount /api/repo_layouts answers
// the plain 404 envelope (its router wiring lives in router.go); the live
// mount is /api/admin/repolayouts — the UI-rest family the official docs
// never published. Read arm only: the write verbs (POST/PUT/DELETE/
// testArtPath/resolveRegex) are decompile-sourced medium confidence and
// wait for a live arm before implementation.

import (
	"net/http"
	"sort"

	"github.com/lzwzzy/binflow/internal/auth"
)

// repoLayoutActions is the list face's per-layout action matrix (the wire
// object, captured live): copy is the built-ins' universal right, edit and
// delete open only on the layouts the reference itself owns as editable.
type repoLayoutActions struct {
	Copy   bool `json:"copy"`
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
}

// repoLayout is one built-in layout's full record — the single-get shape
// (spec 2.1.8 + the conductor's single-read wire capture): name,
// artifactPathPattern, distinctiveDescriptorPathPattern (BOOLEAN — true
// marks the descriptor-carrying layouts), descriptorPathPattern (present
// only on those), the two integration-revision regexps (always present),
// and — outside this struct, in the detail wrapper — the usage-derived
// repositoryAssociations. Actions rides json:"-": the list face projects
// it, the full record (per the captured six/seven-key shape) does not.
type repoLayout struct {
	Name                             string            `json:"name"`
	ArtifactPathPattern              string            `json:"artifactPathPattern"`
	DistinctiveDescriptorPathPattern bool              `json:"distinctiveDescriptorPathPattern"`
	DescriptorPathPattern            string            `json:"descriptorPathPattern,omitempty"`
	FolderIntegrationRevisionRegExp  string            `json:"folderIntegrationRevisionRegExp"`
	FileIntegrationRevisionRegExp    string            `json:"fileIntegrationRevisionRegExp"`
	Actions                          repoLayoutActions `json:"-"`
}

// builtinRepoLayouts is the built-in layout table — all 25 rows transcribed
// verbatim from the conductor's live reference captures (order preserved):
// the list fields from reports/compatibility/l025a-wire/
// replolayouts-reference.json and the single-read fields from
// repolayouts-single-read.json (172.16.58.130:8082, 2026-09-17). The
// terraform-provider row's pattern carries the reference's own
// leading/trailing newline+indent bytes — kept verbatim on purpose. The
// capture file's extra probe names (gradle-default, helm-default,
// pypi-default, …) all answered the reference's own 500 "No value present"
// — they are not layouts of this instance, so they stay out of the table
// and answer BinFlow's same 500 here.
var builtinRepoLayouts = []repoLayout{
	{
		Name:                             "maven-2-default",
		ArtifactPathPattern:              "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
		DistinctiveDescriptorPathPattern: true,
		DescriptorPathPattern:            "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).pom",
		FolderIntegrationRevisionRegExp:  "SNAPSHOT",
		FileIntegrationRevisionRegExp:    "SNAPSHOT|(?:(?:[0-9]{8}.[0-9]{6})-(?:[0-9]+))",
	},
	{
		Name:                             "ivy-default",
		ArtifactPathPattern:              "[org]/[module]/[baseRev](-[folderItegRev])/[type]s/[module](-[classifier])-[baseRev](-[fileItegRev]).[ext]",
		DistinctiveDescriptorPathPattern: true,
		DescriptorPathPattern:            "[org]/[module]/[baseRev](-[folderItegRev])/[type]s/ivy-[baseRev](-[fileItegRev]).xml",
		FolderIntegrationRevisionRegExp:  `\d{14}`,
		FileIntegrationRevisionRegExp:    `\d{14}`,
	},
	{
		Name:                             "maven-1-default",
		ArtifactPathPattern:              "[org]/[type]s/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
		DistinctiveDescriptorPathPattern: true,
		DescriptorPathPattern:            "[org]/[type]s/[module]-[baseRev](-[fileItegRev]).pom",
		FolderIntegrationRevisionRegExp:  ".+",
		FileIntegrationRevisionRegExp:    ".+",
	},
	{
		Name:                            "nuget-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[module].[baseRev](-[fileItegRev]).nupkg",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "npm-default",
		ArtifactPathPattern:             "[orgPath]/-/[module]-[baseRev](-[fileItegRev]).tgz",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "bower-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[module]-[baseRev](-[fileItegRev]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "vcs-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[refs<tags|branches>]/[baseRev]/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   "[a-zA-Z0-9]{40}",
	},
	{
		Name:                             "sbt-default",
		ArtifactPathPattern:              "[org]/[module]/(scala_[scalaVersion<.+>])/(sbt_[sbtVersion<.+>])/[baseRev]/[type]s/[module](-[classifier]).[ext]",
		DistinctiveDescriptorPathPattern: true,
		DescriptorPathPattern:            "[org]/[module]/(scala_[scalaVersion<.+>])/(sbt_[sbtVersion<.+>])/[baseRev]/[type]s/ivy.xml",
		FolderIntegrationRevisionRegExp:  `\d{14}`,
		FileIntegrationRevisionRegExp:    `\d{14}`,
	},
	{
		Name:                            "simple-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[module]-[baseRev].[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "cargo-default",
		ArtifactPathPattern:             "crates/[module]/[module]-[baseRev].[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "composer-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[module]-[baseRev](-[fileItegRev]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
		Actions:                         repoLayoutActions{Copy: true, Edit: true, Delete: true},
	},
	{
		Name:                            "conan-default",
		ArtifactPathPattern:             "[org]/[module]/[baseRev]/[channel<[^/]+>]/[folderItegRev]/(package/[package_id<[^/]+>]/[fileItegRev]/)[remainder<(?:.+)>]",
		FolderIntegrationRevisionRegExp: "[^/]+",
		FileIntegrationRevisionRegExp:   "[^/]+",
	},
	{
		Name:                            "puppet-default",
		ArtifactPathPattern:             "[orgPath]/[module]/[orgPath]-[module]-[baseRev].tar.gz",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "go-default",
		ArtifactPathPattern:             "[orgPath]/[module]/@v/v[baseRev](-[fileItegRev]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "build-default",
		ArtifactPathPattern:             "[orgPath]/[module](-[fileItegRev]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
		Actions:                         repoLayoutActions{Copy: true, Edit: true, Delete: true},
	},
	{
		Name:                            "terraform-module-default",
		ArtifactPathPattern:             "[namespace]/[module-name]/[provider]/[version].[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "terraform-provider-default",
		ArtifactPathPattern:             "\n                [namespace]/[provider-name]/[version]/terraform-provider-[provider-name]_[version]_[os]_[arch].[ext]\n            ",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "swift-default",
		ArtifactPathPattern:             "[scope]/[name]/[name]-[version].zip",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "ansible-default",
		ArtifactPathPattern:             "collections/[namespace]/[name]/[version]/[namespace]-[name]-[version].tar.gz",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                             "sbt-ivy-default",
		ArtifactPathPattern:              "[orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]",
		DistinctiveDescriptorPathPattern: true,
		DescriptorPathPattern:            "[orgPath]/[module]/[baseRev](-[folderItegRev])/ivy-[baseRev](-[fileItegRev])(-[classifier]).xml",
		FolderIntegrationRevisionRegExp:  `\d{14}`,
		FileIntegrationRevisionRegExp:    `\d{14}`,
	},
	{
		Name:                            "nix-default",
		ArtifactPathPattern:             "binary-cache/[narinfoHash]/[narinfoHash].narinfo",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "skills-default",
		ArtifactPathPattern:             "[module]/[version]/[module]-[version].[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "agent-plugins-default",
		ArtifactPathPattern:             "[module]/[version]/[module]-[version].[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "agent-packages-default",
		ArtifactPathPattern:             "[scope]/[package-name]/[package-name]-[version].zip",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
	{
		Name:                            "luarocks-default",
		ArtifactPathPattern:             "[module]/[module]-[baseRev](-[fileItegRev]).[ext]",
		FolderIntegrationRevisionRegExp: ".*",
		FileIntegrationRevisionRegExp:   ".*",
	},
}

// copyOnlyActions is the default action matrix of the reference's built-in
// rows (everything except the two editable ones pinned in the table).
var copyOnlyActions = repoLayoutActions{Copy: true}

// repoLayoutListItem is the list face's row: {name, artifactPathPattern,
// layoutActions} — the captured wire shape.
type repoLayoutListItem struct {
	Name                string            `json:"name"`
	ArtifactPathPattern string            `json:"artifactPathPattern"`
	LayoutActions       repoLayoutActions `json:"layoutActions"`
}

// repoLayoutAssociations is the single-get's association block: the three
// class-scoped lists of repositories whose effective repoLayoutRef names
// this layout (usage-derived — the capture's simple-default carries the
// instance's example-repo-local; a fresh instance renders three empty
// arrays, the spec's original observation).
type repoLayoutAssociations struct {
	LocalRepositories   []string `json:"localRepositories"`
	RemoteRepositories  []string `json:"remoteRepositories"`
	VirtualRepositories []string `json:"virtualRepositories"`
}

// repoLayoutDetail is the single-get body: the full layout record plus the
// association block.
type repoLayoutDetail struct {
	repoLayout
	RepositoryAssociations repoLayoutAssociations `json:"repositoryAssociations"`
}

// handleRepoLayoutsList serves GET /api/admin/repolayouts (spec 2.1.8).
// The read gate is any-project-admin; BinFlow has no project
// administrations, so the passing set is the system admin alone and the
// refusal is the family's 403 "Forbidden" envelope.
func (s *Server) handleRepoLayoutsList(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r.Context(), principalFrom(r.Context()), auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	items := make([]repoLayoutListItem, 0, len(builtinRepoLayouts))
	for _, l := range builtinRepoLayouts {
		actions := l.Actions
		if actions == (repoLayoutActions{}) {
			actions = copyOnlyActions
		}
		items = append(items, repoLayoutListItem{
			Name: l.Name, ArtifactPathPattern: l.ArtifactPathPattern, LayoutActions: actions,
		})
	}
	writeJSONBody(w, http.StatusOK, items)
}

// handleRepoLayoutGet serves GET /api/admin/repolayouts/{key}: the full
// layout record plus the usage-derived associations (every repository whose
// stored repoLayoutRef names the layout, classed by rclass, sorted by key).
// An unknown name answers the reference's verbatim 500 "No value present"
// envelope — the internal Optional.get bug the spec pins as a
// compatibility point (the capture's non-instance probe names —
// gradle-default, pypi-default, … — answer it on the reference too).
func (s *Server) handleRepoLayoutGet(w http.ResponseWriter, r *http.Request, name string) {
	if !s.canManage(r.Context(), principalFrom(r.Context()), auth.CapRepoWrite) {
		writeError(w, http.StatusForbidden, "Forbidden")
		return
	}
	for i := range builtinRepoLayouts {
		l := &builtinRepoLayouts[i]
		if l.Name != name {
			continue
		}
		assoc, err := s.layoutAssociations(r, name)
		if err != nil {
			s.writeRepoSvcError(w, err)
			return
		}
		writeJSONBody(w, http.StatusOK, repoLayoutDetail{repoLayout: *l, RepositoryAssociations: assoc})
		return
	}
	writeError(w, http.StatusInternalServerError, "No value present")
}

// layoutAssociations derives one layout's association block: the
// repositories whose stored config blob carries repoLayoutRef == name,
// classed by rclass and sorted by key (the reference's own mechanism per
// the single-read capture).
func (s *Server) layoutAssociations(r *http.Request, name string) (repoLayoutAssociations, error) {
	assoc := repoLayoutAssociations{
		LocalRepositories:   []string{},
		RemoteRepositories:  []string{},
		VirtualRepositories: []string{},
	}
	rows, err := s.deps.ReposSvc.ListRepos(r.Context(), principalFrom(r.Context()))
	if err != nil {
		return repoLayoutAssociations{}, err
	}
	for _, row := range rows {
		if blobString(repoRowBlob(row), "repoLayoutRef") != name {
			continue
		}
		switch row.Type {
		case "remote":
			assoc.RemoteRepositories = append(assoc.RemoteRepositories, row.RepoKey)
		case "virtual":
			assoc.VirtualRepositories = append(assoc.VirtualRepositories, row.RepoKey)
		default:
			assoc.LocalRepositories = append(assoc.LocalRepositories, row.RepoKey)
		}
	}
	sort.Strings(assoc.LocalRepositories)
	sort.Strings(assoc.RemoteRepositories)
	sort.Strings(assoc.VirtualRepositories)
	return assoc, nil
}
