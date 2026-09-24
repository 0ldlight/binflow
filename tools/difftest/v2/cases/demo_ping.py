"""Demo case: GET /api/system/ping on both sides (layer L0, HTTP primitive).

Declarative format — no code beyond the CASE dict. Endpoints and credentials
come exclusively from A_BASE/B_BASE/A_USER/A_PASSWORD/B_USER/B_PASSWORD.
"""

CASE = {
    "id": "demo-ping",
    "title": "GET /api/system/ping — status + Content-Type + body literal",
    "layer": "L0",
    "domain": None,        # no normalize domain -> raw comparison by design
    "auth": True,          # needs all six env vars -> exercises full env contract
    "timeout_s": 10,
    "requests": [
        {"method": "GET", "path": "/api/system/ping"},
    ],
    "compare": {
        "status": True,
        "headers": ["Content-Type"],
        "body": "literal",
    },
}
