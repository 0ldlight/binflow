// Package maven is the Maven 2 repository adapter (M3, FR-16), mounted on
// the shared content namespace /binflow/<maven-repo>/<layout-path> — like
// generic it has no /api prefix, because Maven clients address repositories
// by plain storage paths (maven-npm-pypi.md section 1.1, high confidence).
//
// # Layout (maven-2-default, the six-field RepoLayout model)
//
// Every content path is strictly validated against the maven-2-default
// template before any verb runs (PRD FR-16, C2 interim 400):
//
//	[orgPath]/[module]/[baseRev](-SNAPSHOT)/
//	    [module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]
//
// with folderIntegrationRevisionRegExp = "SNAPSHOT" and
// fileIntegrationRevisionRegExp = "SNAPSHOT|(?:[0-9]{8}.[0-9]{6})-(?:[0-9]+)"
// (the unique/timestamped snapshot spelling). The classification order is
// fixed by the spec (maven-npm-pypi.md section 1.2, high confidence): strip
// the checksum suffix FIRST, then recognize the metadata file names
// (maven-metadata.xml and the plugin-group metadata-maven-metadata.xml
// variant), and only then match the artifact/descriptor template. A path
// that matches none of the three is a 400 — maven repositories must not
// degrade into generic storage (M20).
//
// # Checksum sidecars and policy
//
// .sha1/.md5/.sha256 files ride the artifact path family: a sidecar PUT is
// checked against the server-measured digest of its TARGET per the
// repository's checksumPolicyType (client-checksums strict 409,
// server-generated-checksums silently accepting with the measured bytes
// landing, rest-api.md section 1.5), and a sidecar GET always answers the
// server-computed bare hex — the stored sidecar bytes only ever register
// the client's original claim (maven-npm-pypi.md section 1.5).
//
// The maven-metadata.xml calculator itself (server-side generation of the
// three metadata levels) is NOT in this package's M3 batch-3 scope: T-68
// owns it. Until it lands, metadata paths behave as ordinary stored nodes
// (a client PUT is accepted per ME-06, a GET serves what was stored).
package maven
