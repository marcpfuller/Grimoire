#!/usr/bin/env python3
"""
seed_ghostwriter.py — Seeds a local Ghostwriter instance with a test client,
project, and oplog, then creates an OPLOG_RW service token for Grimoire testing.

Uses the Hasura `run_sql` admin API (port 8080) for DB operations, which
bypasses the JWT-based GraphQL auth that Ghostwriter wraps around /v1/graphql.
Service token creation uses `docker compose run django shell` since Ghostwriter's
token model generates and hashes the raw secret in Python.

Usage:
    python scripts/seed_ghostwriter.py [options]

Options:
    --hasura-url      Direct Hasura URL  (default: http://localhost:8080)
    --admin-secret    Hasura admin secret (default: reads from GW .env)
    --gw-url          Ghostwriter URL for output (default: http://localhost:8000)
    --compose-file    Docker Compose file (default: local.yml)
    --compose-dir     Directory with Compose file (default: ../myrepos/Ghostwriter)
    --client-name     (default: "Grimoire Test Client")
    --project-name    Project codename (default: "GRIMOIRE_TEST")
    --oplog-name      (default: "Grimoire Test Oplog")
    --token-name      (default: "Grimoire Test Token")

Note on project_id:
    Ghostwriter oplogs are scoped to a project, but Grimoire's PushCommand does
    not pass project_id when inserting entries — the oplog already carries that
    association. One service token → one oplog. For multiple Ghostwriter projects,
    configure separate tokens per project in a future multi-token config.
"""

import argparse
import json
import os
import subprocess
import sys
import textwrap
from datetime import date, timedelta
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

_HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_HASURA_URL   = os.environ.get("HASURA_URL", "http://localhost:8080")
DEFAULT_ADMIN_SECRET = os.environ.get("HASURA_GRAPHQL_ADMIN_SECRET", "4V44aoY858LjGDslWpB0CORSUXgN4Uzg")
DEFAULT_COMPOSE_FILE = os.environ.get("GW_COMPOSE_FILE", "local.yml")
DEFAULT_COMPOSE_DIR  = os.environ.get(
    "GW_COMPOSE_DIR",
    os.path.normpath(os.path.join(_HERE, "..", "..", "myrepos", "Ghostwriter")),
)
DEFAULT_GW_URL = os.environ.get("GW_URL", "http://localhost:8000")


# ---------------------------------------------------------------------------
# SQL helper — uses Hasura /v2/query run_sql (no JWT needed, admin secret only)
# ---------------------------------------------------------------------------

def _sql(hasura_url: str, admin_secret: str, query: str) -> list:
    payload = json.dumps({"type": "run_sql", "args": {"sql": query, "source": "default"}}).encode()
    req = Request(
        hasura_url.rstrip("/") + "/v2/query",
        data=payload,
        headers={"Content-Type": "application/json", "x-hasura-admin-secret": admin_secret},
        method="POST",
    )
    try:
        with urlopen(req, timeout=15) as resp:
            result = json.loads(resp.read())
    except HTTPError as exc:
        sys.exit(f"[error] Hasura HTTP {exc.code}: {exc.read().decode()[:400]}")
    except URLError as exc:
        sys.exit(f"[error] Cannot reach {hasura_url}: {exc.reason}\n"
                 "       Is Ghostwriter running?  (./ghostwriter-cli-macos up --dev)")
    if "error" in result:
        sys.exit(f"[error] SQL: {result['error']}")
    return result.get("result", [])

def _scalar(rows: list, default=None):
    return rows[1][0] if len(rows) > 1 and rows[1] else default

def _q(s: str) -> str:
    return "'" + str(s).replace("'", "''") + "'"


# ---------------------------------------------------------------------------
# Setup steps
# ---------------------------------------------------------------------------

def find_or_create_client(h: str, secret: str, name: str) -> int:
    rows = _sql(h, secret, f"SELECT id FROM rolodex_client WHERE name={_q(name)} LIMIT 1;")
    if _scalar(rows):
        cid = int(_scalar(rows))
        print(f"  [ok] reusing client id={cid}  name={name!r}")
        return cid
    rows = _sql(h, secret, f"""
        INSERT INTO rolodex_client (name, short_name, codename, description, address, timezone, extra_fields)
        VALUES ({_q(name)}, {_q(name[:20])}, '', '', '', 'UTC', '{{}}') RETURNING id;
    """)
    cid = int(_scalar(rows))
    print(f"  [ok] created client id={cid}  name={name!r}")
    return cid

def find_or_create_project_type(h: str, secret: str) -> int:
    rows = _sql(h, secret, "SELECT id FROM rolodex_projecttype LIMIT 1;")
    if _scalar(rows):
        tid = int(_scalar(rows))
        print(f"  [ok] using project type id={tid}")
        return tid
    rows = _sql(h, secret, "INSERT INTO rolodex_projecttype (project_type) VALUES ('Red Team') RETURNING id;")
    tid = int(_scalar(rows))
    print(f"  [ok] created project type id={tid}")
    return tid

def find_superuser_id(h: str, secret: str) -> int:
    rows = _sql(h, secret, "SELECT id FROM users_user WHERE is_superuser=TRUE ORDER BY id LIMIT 1;")
    if not _scalar(rows):
        sys.exit("[error] No superuser found — run Ghostwriter install first.")
    uid = int(_scalar(rows))
    print(f"  [ok] operator id={uid}")
    return uid

def find_or_create_project(h: str, secret: str, codename: str, client_id: int, type_id: int, operator_id: int) -> int:
    rows = _sql(h, secret, f"SELECT id FROM rolodex_project WHERE codename={_q(codename)} LIMIT 1;")
    if _scalar(rows):
        pid = int(_scalar(rows))
        print(f"  [ok] reusing project id={pid}  codename={codename!r}")
        return pid
    today = date.today().isoformat()
    end   = (date.today() + timedelta(days=30)).isoformat()
    rows = _sql(h, secret, f"""
        INSERT INTO rolodex_project
          (codename, start_date, end_date, slack_channel, description, extra_fields,
           client_id, project_type_id, operator_id)
        VALUES ({_q(codename)}, '{today}', '{end}', '', '', '{{}}',
                {client_id}, {type_id}, {operator_id})
        RETURNING id;
    """)
    pid = int(_scalar(rows))
    print(f"  [ok] created project id={pid}  codename={codename!r}")
    return pid

def find_or_create_oplog(h: str, secret: str, name: str, project_id: int) -> int:
    rows = _sql(h, secret, f"""
        SELECT id FROM oplog_oplog WHERE name={_q(name)} AND project_id={project_id} LIMIT 1;
    """)
    if _scalar(rows):
        oid = int(_scalar(rows))
        print(f"  [ok] reusing oplog id={oid}  name={name!r}")
        return oid
    rows = _sql(h, secret, f"""
        INSERT INTO oplog_oplog (name, project_id, mute_notifications)
        VALUES ({_q(name)}, {project_id}, FALSE) RETURNING id;
    """)
    oid = int(_scalar(rows))
    print(f"  [ok] created oplog id={oid}  name={name!r}")
    return oid


# ---------------------------------------------------------------------------
# Service token via docker compose (Django ORM handles hashing + prefix)
# ---------------------------------------------------------------------------

def create_service_token(compose_dir: str, compose_file: str, oplog_id: int, token_name: str) -> str:
    snippet = textwrap.dedent(f"""
        import sys
        from django.contrib.auth import get_user_model
        from ghostwriter.api.models import ServicePrincipal, ServiceToken, ServiceTokenPreset
        User = get_user_model()
        user = User.objects.filter(is_superuser=True).first()
        if not user:
            print("ERROR: no superuser", file=sys.stderr); sys.exit(1)
        principal, _ = ServicePrincipal.objects.get_or_create(
            name={token_name!r}, created_by=user,
            defaults={{"service_type": ServicePrincipal.ServiceType.INTEGRATION, "active": True}},
        )
        ServiceToken.objects.filter(name={token_name!r}, revoked=False).update(revoked=True)
        perms = ServiceToken.build_permissions_for_preset(
            ServiceTokenPreset.OPLOG_RW, oplog_id={oplog_id}
        )
        _, raw = ServiceToken.objects.create_token(
            name={token_name!r}, service_principal=principal, created_by=user, permissions=perms
        )
        print(raw)
    """).strip()

    cmd = ["docker", "compose", "-f", compose_file,
           "run", "--rm", "django", "python", "manage.py", "shell", "-c", snippet]
    print(f"  [run] docker compose run django shell  (oplog_id={oplog_id})")
    try:
        result = subprocess.run(cmd, cwd=os.path.abspath(compose_dir),
                                capture_output=True, text=True, timeout=120)
    except FileNotFoundError:
        sys.exit("[error] `docker` not found.")
    except subprocess.TimeoutExpired:
        sys.exit("[error] Timed out waiting for Django shell.")

    if result.returncode != 0:
        sys.exit(f"[error] Django shell exited {result.returncode}:\n{result.stderr.strip()}")

    token = result.stdout.strip().splitlines()[-1]
    if not token or " " in token or len(token) < 20:
        sys.exit(f"[error] Unexpected token output:\n  stdout={result.stdout!r}\n  stderr={result.stderr!r}")
    return token


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    p = argparse.ArgumentParser(description=__doc__.split("\n")[1].strip(),
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--hasura-url",    default=DEFAULT_HASURA_URL)
    p.add_argument("--admin-secret",  default=DEFAULT_ADMIN_SECRET)
    p.add_argument("--gw-url",        default=DEFAULT_GW_URL)
    p.add_argument("--compose-file",  default=DEFAULT_COMPOSE_FILE)
    p.add_argument("--compose-dir",   default=DEFAULT_COMPOSE_DIR)
    p.add_argument("--client-name",   default="Grimoire Test Client")
    p.add_argument("--project-name",  default="GRIMOIRE_TEST")
    p.add_argument("--oplog-name",    default="Grimoire Test Oplog")
    p.add_argument("--token-name",    default="Grimoire Test Token")
    args = p.parse_args()

    print(f"\nSeeding Ghostwriter via Hasura at {args.hasura_url}\n")

    print("1/5  Client …");       client_id   = find_or_create_client(args.hasura_url, args.admin_secret, args.client_name)
    print("2/5  Project type …"); type_id     = find_or_create_project_type(args.hasura_url, args.admin_secret)
    print("3/5  Operator …");     operator_id = find_superuser_id(args.hasura_url, args.admin_secret)
    print("4/5  Project …");      project_id  = find_or_create_project(args.hasura_url, args.admin_secret, args.project_name, client_id, type_id, operator_id)
    print("5/5  Oplog …");        oplog_id    = find_or_create_oplog(args.hasura_url, args.admin_secret, args.oplog_name, project_id)

    print("\n6/6  Service token (OPLOG_RW) …")
    token = create_service_token(args.compose_dir, args.compose_file, oplog_id, args.token_name)
    print("  [ok] token created")

    print("\n" + "=" * 60)
    print("Grimoire config — run these commands:")
    print("=" * 60)
    print(f"  grim config set ghostwriter.url {args.gw_url}")
    print(f"  grim config set ghostwriter.token {token}")
    print("=" * 60)
    print(f"\nOplog URL: {args.gw_url}/oplog/{oplog_id}/entries")
    print(f"project_id={project_id}  oplog_id={oplog_id}\n")

if __name__ == "__main__":
    main()
