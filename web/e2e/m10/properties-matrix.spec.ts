import { test } from '@playwright/test'

// T-277 skeleton (M10 B0): properties-system slot. The LEGACY ';' fixture
// data this spec's L22 leg consumes is already real: web/scripts/seed-m10.mjs
// (see e2e/m10/support/seed.ts — fixtureBody/legacyFixtures are importable
// today).
//
// Legs (PRD §5.5, FR-89; stripping grammar per ADR-0033 / architecture
// §15.3.1):
//   L18 matrix params: PUT app.bin;build=77;env=prod -> artifact a/b/app.bin
//      + ?properties=build,env reads {"build":["77"],"env":["prod"]}
//   L19 REST read/write/delete: PUT ?properties=qa=passed,owner=team-a /
//      DELETE ?properties=qa / wildcard build* / folder recursive=1 /
//      atomic missing-key 404
//   L20 validation: ";bad key=1" -> 400 (matrix + REST arms); >500 props 400
//   L21 UI: NodeDetail Properties Tab render/edit/delete + readonly posture
//      (fills with the FE ticket; anchors enter console-ux §10 first)
//   L22 legacy regression: the seed-m10 fixtures GET their ORIGINAL literal
//      paths 200 byte-identical (seed-m10 verifyM10 already gates this —
//      this leg re-asserts post-FR-89 plus the storage.matrix_params=off
//      escape-hatch arm)
//
// Fills with the FR-89 BE/FE tickets.

test.skip('properties: matrix-param deploy + REST CRUD + UI tab + legacy semicolon regression (fills with the FR-89 tickets)', () => {
  // Placeholder body intentionally empty — T-277 lands only the skeleton.
})
