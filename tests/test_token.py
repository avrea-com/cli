"""Unit tests for the `avr token` command group."""

from avrea_cli.docs_gen import build_tree
from avrea_cli.main import cli
from uuid import UUID
import json
import pytest

TOKENS = "/orgs/org-default/access-tokens"
CREDENTIAL = "avs_0123456789abcdefghijklmnopqrstuv"
TOKEN_ID = f"key-{UUID('019a0000-0000-7000-8000-000000000001').hex}"
OTHER_TOKEN_ID = f"key-{UUID('019a0000-0000-7000-8000-000000000002').hex}"
THIRD_TOKEN_ID = f"key-{UUID('019a0000-0000-7000-8000-000000000003').hex}"
UNKNOWN_TOKEN_ID = f"key-{UUID('019a0000-0000-7000-8000-000000000009').hex}"

REPO_GRANT = {"resource_type": "repository", "resource_id": "rep-aaa", "access_level": "read"}
VM_GRANT = {"resource_type": "customer_vm", "resource_id": "cvm-1", "access_level": "admin"}


def _token(**overrides):
    """A token object as the API returns it from list and view."""
    token = {
        "id": TOKEN_ID,
        "organization_id": "org-default",
        "user_id": "usr-1",
        "name": "ci-read",
        "created_at": "2026-10-07T10:00:00Z",
        "expires_at": "2099-01-01T00:00:00Z",
        "last_used_at": None,
        "revoked_at": None,
        "revoked_reason": None,
        "allow_vm_create": False,
        "vm_create_limit": None,
        "vm_create_count": 0,
        "grants": [REPO_GRANT],
    }
    token.update(overrides)
    return token


def _minted(**overrides):
    """The create response: the token object plus the one-time credential."""
    return {"token": CREDENTIAL, **_token(**overrides)}


def _create(runner, *args, **kwargs):
    return runner.invoke(cli, ["token", "create", "--name", "ci-read", *args], **kwargs)


def _body(api):
    (request,) = api.sent("POST", TOKENS)
    return json.loads(request.content)


class TestTokenCreateRequest:
    def test_repository_id_grant(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "rep-aaa")
        assert result.exit_code == 0, result.output
        # The TTL and the VM-create limit are left out so the server defaults apply.
        assert _body(api) == {"name": "ci-read", "grants": [REPO_GRANT], "allow_vm_create": False}
        assert [f"{r.method} {r.url.path}" for r in api.requests] == [f"POST {TOKENS}"]

    def test_several_repositories_and_vms_in_one_call(self, runner, api):
        api.reply("GET", "/orgs/org-default/repos/resolve", json={"data": {"repository_id": "rep-aaa"}})
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "acme/web", "--repo", "rep-bbb", "--vm", "cvm-1", "--vm", "cvm-2")
        assert result.exit_code == 0, result.output
        (resolve,) = api.sent("GET", "/orgs/org-default/repos/resolve")
        assert resolve.url.params["name"] == "acme/web"
        assert _body(api)["grants"] == [
            REPO_GRANT,
            {"resource_type": "repository", "resource_id": "rep-bbb", "access_level": "read"},
            VM_GRANT,
            {"resource_type": "customer_vm", "resource_id": "cvm-2", "access_level": "admin"},
        ]

    def test_level_suffix_of_the_minted_level_is_accepted(self, runner, api):
        api.reply("GET", "/orgs/org-default/repos/resolve", json={"data": {"repository_id": "rep-aaa"}})
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "acme/web:read", "--repo", "rep-bbb:read", "--vm", "cvm-1:admin")
        assert result.exit_code == 0, result.output
        (resolve,) = api.sent("GET", "/orgs/org-default/repos/resolve")
        assert resolve.url.params["name"] == "acme/web"
        assert _body(api)["grants"] == [
            REPO_GRANT,
            {"resource_type": "repository", "resource_id": "rep-bbb", "access_level": "read"},
            VM_GRANT,
        ]

    @pytest.mark.parametrize(
        ("args", "minted_level"),
        [
            (["--repo", "acme/web:write"], "read"),
            (["--repo", "rep-aaa:admin"], "read"),
            (["--repo", "acme/web:"], "read"),
            (["--vm", "cvm-1:read"], "admin"),
            (["--vm", "cvm-1:write"], "admin"),
            (["--repo", "rep-aaa", "--vm", "cvm-1:owner"], "admin"),
        ],
    )
    def test_other_level_suffix_is_a_usage_error_naming_the_minted_level(self, runner, api, args, minted_level):
        result = _create(runner, *args)
        assert result.exit_code == 2
        assert minted_level in result.stderr
        assert api.requests == []

    def test_the_same_grant_twice_is_sent_once(self, runner, api):
        api.reply("GET", "/orgs/org-default/repos/resolve", json={"data": {"repository_id": "rep-aaa"}})
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "acme/web", "--repo", "rep-aaa", "--vm", "cvm-1", "--vm", "cvm-1:admin")
        assert result.exit_code == 0, result.output
        assert _body(api)["grants"] == [REPO_GRANT, VM_GRANT]

    def test_vm_create_only(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted(grants=[], allow_vm_create=True))
        result = _create(runner, "--allow-vm-create")
        assert result.exit_code == 0, result.output
        assert _body(api) == {"name": "ci-read", "grants": [], "allow_vm_create": True}

    @pytest.mark.parametrize("limit", [1, 10])
    def test_vm_create_limit_bounds_accepted(self, runner, api, limit):
        api.reply("POST", TOKENS, 201, json=_minted(grants=[], allow_vm_create=True, vm_create_limit=limit))
        result = _create(runner, "--allow-vm-create", "--vm-create-limit", str(limit))
        assert result.exit_code == 0, result.output
        assert _body(api) == {"name": "ci-read", "grants": [], "allow_vm_create": True, "vm_create_limit": limit}

    @pytest.mark.parametrize("limit", ["0", "11", "-1", "many"])
    def test_vm_create_limit_out_of_range_is_a_usage_error(self, runner, api, limit):
        result = _create(runner, "--allow-vm-create", "--vm-create-limit", limit)
        assert result.exit_code == 2
        assert api.requests == []

    def test_vm_create_limit_without_allow_vm_create_is_a_usage_error(self, runner, api):
        result = _create(runner, "--repo", "rep-aaa", "--vm-create-limit", "3")
        assert result.exit_code == 2
        assert "--allow-vm-create" in result.stderr
        assert api.requests == []

    def test_no_grant_at_all_is_a_usage_error(self, runner, api):
        result = _create(runner)
        assert result.exit_code == 2
        for flag in ("--repo", "--vm", "--allow-vm-create"):
            assert flag in result.stderr
        assert api.requests == []

    def test_name_is_required(self, runner, api):
        result = runner.invoke(cli, ["token", "create", "--repo", "rep-aaa"])
        assert result.exit_code == 2
        assert "--name" in result.stderr
        assert api.requests == []

    def test_more_than_100_grants_is_a_usage_error(self, runner, api):
        args = [arg for n in range(101) for arg in ("--vm", f"cvm-{n}")]
        result = _create(runner, *args)
        assert result.exit_code == 2
        assert "100" in result.stderr
        assert api.requests == []

    def test_all_options_together(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(
            runner, "--repo", "rep-aaa", "--vm", "cvm-1", "--allow-vm-create", "--vm-create-limit", "3", "--ttl", "30m"
        )
        assert result.exit_code == 0, result.output
        assert _body(api) == {
            "name": "ci-read",
            "grants": [REPO_GRANT, VM_GRANT],
            "allow_vm_create": True,
            "vm_create_limit": 3,
            "ttl_seconds": 1800,
        }

    def test_org_option_selects_the_organization(self, runner, api):
        api.reply("POST", "/orgs/org-other/access-tokens", 201, json=_minted(organization_id="org-other"))
        result = _create(runner, "--repo", "rep-aaa", "--org", "org-other")
        assert result.exit_code == 0, result.output
        assert len(api.sent("POST", "/orgs/org-other/access-tokens")) == 1

    def test_requires_auth(self, runner, api, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = _create(runner, "--repo", "rep-aaa")
        assert result.exit_code == 4
        assert "avr auth login" in result.stderr
        assert api.requests == []


class TestTokenCreateTtl:
    @pytest.mark.parametrize(
        ("ttl", "seconds"),
        [
            ("60", 60),
            ("60s", 60),
            ("1m", 60),
            ("30m", 1800),
            ("8h", 28800),
            ("3600", 3600),
            ("7d", 604800),
            ("168h", 604800),
            ("604800", 604800),
        ],
    )
    def test_accepted_forms_and_boundaries(self, runner, api, ttl, seconds):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "rep-aaa", "--ttl", ttl)
        assert result.exit_code == 0, result.output
        assert _body(api)["ttl_seconds"] == seconds

    @pytest.mark.parametrize(
        "ttl", ["59", "59s", "0", "604801", "604801s", "8d", "169h", "soon", "", "1.5h", "-5m", "m"]
    )
    def test_refused_values(self, runner, api, ttl):
        result = _create(runner, "--repo", "rep-aaa", f"--ttl={ttl}")
        assert result.exit_code == 2
        assert "--ttl" in result.stderr
        assert api.requests == []

    def test_omitted_ttl_leaves_the_server_default(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "rep-aaa")
        assert result.exit_code == 0, result.output
        assert "ttl_seconds" not in _body(api)


class TestTokenCreateOutput:
    def test_human_output_shows_the_credential_exactly_once(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted(grants=[REPO_GRANT, VM_GRANT]))
        result = _create(runner, "--repo", "rep-aaa", "--vm", "cvm-1")
        assert result.exit_code == 0, result.output
        assert result.output.count(CREDENTIAL) == 1
        assert CREDENTIAL not in result.stderr
        lines = [line.strip() for line in result.stdout.splitlines()]
        assert f"export AVR_TOKEN={CREDENTIAL}" in lines
        assert "export AVR_ORG=org-default" in lines
        assert "once" in result.stdout
        for expected in (
            TOKEN_ID,
            "ci-read",
            "2099-01-01",
            "rep-aaa",
            "read",
            "cvm-1",
            "admin",
        ):
            assert expected in result.stdout

    def test_export_names_the_organization_id_when_a_slug_was_given(self, runner, api):
        api.reply("GET", "/users/me/organizations", json={"data": [{"organization_id": "org-acme", "slug": "acme"}]})
        api.reply("POST", "/orgs/org-acme/access-tokens", 201, json=_minted(organization_id="org-acme"))
        result = _create(runner, "--repo", "rep-aaa", "--org", "acme")
        assert result.exit_code == 0, result.output
        assert "export AVR_ORG=org-acme" in [line.strip() for line in result.stdout.splitlines()]

    def test_verbose_logging_never_carries_the_credential(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = runner.invoke(cli, ["--verbose", "token", "create", "--name", "ci-read", "--repo", "rep-aaa"])
        assert result.exit_code == 0, result.output
        assert "POST" in result.stderr
        assert CREDENTIAL not in result.stderr
        assert result.output.count(CREDENTIAL) == 1

    def test_json_output_carries_the_credential_exactly_once(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted(grants=[REPO_GRANT, VM_GRANT]))
        result = _create(runner, "--repo", "rep-aaa", "--vm", "cvm-1", "--json", "*")
        assert result.exit_code == 0, result.output
        assert result.output.count(CREDENTIAL) == 1
        assert CREDENTIAL not in result.stderr
        assert json.loads(result.stdout) == _minted(grants=[REPO_GRANT, VM_GRANT])

    def test_json_field_selection(self, runner, api):
        api.reply("POST", TOKENS, 201, json=_minted())
        result = _create(runner, "--repo", "rep-aaa", "--json", "token,id,expires_at")
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "token": CREDENTIAL,
            "id": TOKEN_ID,
            "expires_at": "2099-01-01T00:00:00Z",
        }

    def test_json_without_the_token_field_is_refused_before_minting(self, runner, api):
        """The credential is returned once; a field list that leaves it out
        would mint a token nobody can use."""
        result = _create(runner, "--repo", "rep-aaa", "--json", "id,name")
        assert result.exit_code == 2
        assert "token" in result.stderr
        assert api.requests == []

    def test_json_question_mark_lists_fields_without_minting(self, runner, api):
        result = _create(runner, "--repo", "rep-aaa", "--json", "?")
        assert result.exit_code == 0, result.output
        assert "token" in result.stdout
        assert "expires_at" in result.stdout
        assert api.requests == []


class TestTokenCreateRefusals:
    def _refused(self, runner, api, status, *, detail=None, headers=None):
        body = None if detail is None else {"detail": detail}
        api.reply("POST", TOKENS, status, json=body, headers=headers)
        return _create(runner, "--repo", "rep-aaa", "--vm", "cvm-1")

    CREATE_UNAVAILABLE = (
        "Error: Scoped token creation is not available for this organization (HTTP 404).\n"
        "  To request access, contact support@avrea.com.\n"
    )
    VM_UNAVAILABLE = "  Check whether scoped tokens and customer VMs are enabled for this organization.\n"

    def test_404_without_vm_access_says_creation_is_unavailable_and_how_to_ask(self, runner, api):
        api.reply("POST", TOKENS, 404, json={"detail": "Not Found"})
        result = _create(runner, "--repo", "rep-019a0000000070008000000000000002")
        assert result.exit_code == 1
        assert result.stderr == self.CREATE_UNAVAILABLE
        assert result.stdout == ""

    @pytest.mark.parametrize(
        "args",
        [
            ["--vm", "cvm-019a0000000070008000000000000004"],
            ["--vm", "cvm-019a0000000070008000000000000004", "--vm", "cvm-019a0000000070008000000000000009"],
            ["--allow-vm-create"],
            ["--allow-vm-create", "--vm-create-limit", "2", "--json", "*"],
        ],
    )
    def test_404_with_vm_access_names_the_vm_capability(self, runner, api, args):
        """The API's bare 404 cannot distinguish disabled token and VM support."""
        api.reply("POST", TOKENS, 404, json={"detail": "Not Found"})
        result = _create(runner, *args)
        assert result.exit_code == 1
        assert result.stderr == self.CREATE_UNAVAILABLE + self.VM_UNAVAILABLE
        assert result.stdout == ""

    def test_404_names_a_non_default_api(self, runner, api, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://avrea.example.com")
        api.reply("POST", TOKENS, 404, json={"detail": "Not Found"})
        result = _create(runner, "--repo", "rep-aaa")
        assert result.exit_code == 1
        assert result.stderr == (self.CREATE_UNAVAILABLE + f"  API: https://avrea.example.com{TOKENS}\n")

    def test_403_surfaces_the_refused_grant(self, runner, api):
        detail = "grants[1] names an object you cannot reach at that level"
        result = self._refused(runner, api, 403, detail=detail)
        assert result.exit_code == 1
        assert "HTTP 403" in result.stderr
        assert detail in result.stderr
        # The index means nothing without the order the flags were sent in.
        assert "--repo values first" in result.stderr

    def test_403_for_the_caller_says_nothing_about_grant_order(self, runner, api):
        result = self._refused(runner, api, 403, detail="Not an active member of this organization")
        assert result.exit_code == 1
        assert "Not an active member of this organization" in result.stderr
        assert "--repo values first" not in result.stderr

    def test_409_names_the_limit_and_the_way_out(self, runner, api):
        result = self._refused(runner, api, 409, detail="Token limit reached")
        assert result.exit_code == 1
        assert "HTTP 409" in result.stderr
        assert "20" in result.stderr
        assert "avr token list" in result.stderr
        assert "avr token revoke" in result.stderr

    def test_422_names_the_field_from_a_validation_list(self, runner, api):
        detail = [
            {"type": "string_too_long", "loc": ["body", "name"], "msg": "String should have at most 100 characters"}
        ]
        result = self._refused(runner, api, 422, detail=detail)
        assert result.exit_code == 1
        assert "HTTP 422" in result.stderr
        assert "name" in result.stderr.split("HTTP 422", 1)[1]
        assert "String should have at most 100 characters" in result.stderr

    def test_422_names_the_refused_grant_by_position(self, runner, api):
        detail = [{"type": "target_not_found", "loc": ["body", "grants", 1], "msg": "target not found"}]
        result = self._refused(runner, api, 422, detail=detail)
        assert result.exit_code == 1
        assert "grants[1]: target not found" in result.stderr
        assert "--repo values first" in result.stderr

    def test_422_surfaces_a_plain_detail(self, runner, api):
        result = self._refused(runner, api, 422, detail="grants[0].resource_id must be a repository id")
        assert result.exit_code == 1
        assert "grants[0].resource_id must be a repository id" in result.stderr

    def test_429_reports_the_retry_delay(self, runner, api):
        result = self._refused(runner, api, 429, detail="Too many tokens minted", headers={"Retry-After": "12"})
        assert result.exit_code == 1
        assert "HTTP 429" in result.stderr
        assert "Retry in 12s" in result.stderr

    def test_503_says_to_retry_after_the_given_delay(self, runner, api):
        detail = "Access for grants[0] could not be verified right now"
        result = self._refused(runner, api, 503, detail=detail, headers={"Retry-After": "7"})
        assert result.exit_code == 1
        assert "HTTP 503" in result.stderr
        assert detail in result.stderr
        assert "No token was created" in result.stderr
        assert "Retry in 7s" in result.stderr

    def test_503_without_retry_after_still_says_to_retry(self, runner, api):
        result = self._refused(runner, api, 503, detail="Scoped tokens are temporarily unavailable")
        assert result.exit_code == 1
        assert "No token was created" in result.stderr
        assert "Retry" in result.stderr

    def test_401_keeps_the_auth_exit_code(self, runner, api):
        result = self._refused(runner, api, 401)
        assert result.exit_code == 4
        assert "avr auth login" in result.stderr

    def test_a_refusal_prints_nothing_on_stdout(self, runner, api):
        result = self._refused(runner, api, 409, detail="Token limit reached")
        assert result.stdout == ""


class TestTokenList:
    def test_table_lists_live_tokens(self, runner, api):
        rows = [
            _token(last_used_at="2020-01-01T00:00:00Z"),
            _token(
                id=OTHER_TOKEN_ID,
                name="vm-admin",
                grants=[VM_GRANT],
                allow_vm_create=True,
                vm_create_limit=3,
            ),
        ]
        api.reply("GET", TOKENS, json={"data": rows, "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 0, result.output
        (request,) = api.sent("GET", TOKENS)
        assert dict(request.url.params) == {"limit": "50"}
        for expected in (
            TOKEN_ID,
            "ci-read",
            OTHER_TOKEN_ID,
            "vm-admin",
            "2099-01-01",
            "never",
            "ago",
        ):
            assert expected in result.stdout
        # Everything listed belongs to one member, so no owner column.
        assert "Owner" not in result.stdout
        assert "usr-1" not in result.stdout

    def test_owner_column_when_other_members_tokens_are_listed(self, runner, api):
        rows = [_token(), _token(id=OTHER_TOKEN_ID, user_id="usr-2")]
        api.reply("GET", TOKENS, json={"data": rows, "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 0, result.output
        assert "Owner" in result.stdout
        assert "usr-1" in result.stdout
        assert "usr-2" in result.stdout

    def test_grants_column_is_compact(self, runner, api):
        grants = [
            REPO_GRANT,
            {"resource_type": "repository", "resource_id": "rep-bbb", "access_level": "read"},
            VM_GRANT,
        ]
        row = _token(grants=grants, allow_vm_create=True, vm_create_limit=3, vm_create_count=1)
        api.reply("GET", TOKENS, json={"data": [row], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 0, result.output
        assert "2 repos, 1 VM, VM create 1/3" in result.stdout
        # The full grant list belongs to `token view`.
        assert "rep-bbb" not in result.stdout

    def test_follows_the_cursor_across_two_pages(self, runner, api):
        api.reply(
            "GET",
            TOKENS,
            json={
                "data": [_token(), _token(id=OTHER_TOKEN_ID)],
                "pagination": {"next_cursor": "page-2"},
            },
        )
        api.reply(
            "GET",
            TOKENS,
            json={"data": [_token(id=THIRD_TOKEN_ID)], "pagination": {"next_cursor": None}},
        )
        result = runner.invoke(cli, ["token", "list", "--json", "id"])
        assert result.exit_code == 0, result.output
        first, second = api.sent("GET", TOKENS)
        assert dict(first.url.params) == {"limit": "50"}
        assert dict(second.url.params) == {"limit": "48", "cursor": "page-2"}
        assert json.loads(result.stdout) == [
            {"id": TOKEN_ID},
            {"id": OTHER_TOKEN_ID},
            {"id": THIRD_TOKEN_ID},
        ]
        assert "more" not in result.stderr.lower()

    def test_stops_at_the_limit_and_says_more_exist(self, runner, api):
        page = {
            "data": [_token(), _token(id=OTHER_TOKEN_ID)],
            "pagination": {"next_cursor": "page-2"},
        }
        api.reply("GET", TOKENS, json=page)
        result = runner.invoke(cli, ["token", "list", "--limit", "2"])
        assert result.exit_code == 0, result.output
        (request,) = api.sent("GET", TOKENS)
        assert dict(request.url.params) == {"limit": "2"}
        assert "--limit" in result.stderr

    def test_a_limit_above_the_page_size_is_split_into_pages(self, runner, api):
        first = [_token(id=f"key-a{n}") for n in range(200)]
        second = [_token(id=f"key-b{n}") for n in range(50)]
        api.reply("GET", TOKENS, json={"data": first, "pagination": {"next_cursor": "page-2"}})
        api.reply("GET", TOKENS, json={"data": second, "pagination": {"next_cursor": "page-3"}})
        result = runner.invoke(cli, ["token", "list", "-L", "250", "--json", "id"])
        assert result.exit_code == 0, result.output
        requests = api.sent("GET", TOKENS)
        assert [dict(r.url.params) for r in requests] == [{"limit": "200"}, {"limit": "50", "cursor": "page-2"}]
        assert len(json.loads(result.stdout)) == 250

    def test_a_page_longer_than_asked_is_cut_to_the_limit(self, runner, api):
        page = {
            "data": [
                _token(),
                _token(id=OTHER_TOKEN_ID),
                _token(id=THIRD_TOKEN_ID),
            ],
            "pagination": {"next_cursor": None},
        }
        api.reply("GET", TOKENS, json=page)
        result = runner.invoke(cli, ["token", "list", "--limit", "2", "--json", "id"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == [
            {"id": TOKEN_ID},
            {"id": OTHER_TOKEN_ID},
        ]

    def test_an_empty_page_ends_the_walk_even_with_a_cursor(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [], "pagination": {"next_cursor": "stuck"}})
        result = runner.invoke(cli, ["token", "list", "--json", "id"])
        assert result.exit_code == 0, result.output
        assert len(api.sent("GET", TOKENS)) == 1
        assert json.loads(result.stdout) == []

    def test_empty_list(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 0, result.output
        assert len(api.sent("GET", TOKENS)) == 1

    def test_json_never_has_a_credential_field(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [_token()], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list", "--json", "*"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == [_token()]
        refused = runner.invoke(cli, ["token", "list", "--json", "token"])
        assert refused.exit_code != 0
        assert "Unknown JSON field" in refused.stderr

    @pytest.mark.parametrize("limit", ["0", "-1", "1001"])
    def test_limit_out_of_range(self, runner, api, limit):
        result = runner.invoke(cli, ["token", "list", "--limit", limit])
        assert result.exit_code == 2
        assert api.requests == []

    def test_org_option(self, runner, api):
        api.reply("GET", "/orgs/org-other/access-tokens", json={"data": [], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["token", "list", "--org", "org-other"])
        assert result.exit_code == 0, result.output
        assert len(api.sent("GET", "/orgs/org-other/access-tokens")) == 1

    def test_404_is_not_read_as_tokens_being_disabled(self, runner, api):
        """Listing stays available where creating tokens is not enabled, so a
        404 here says nothing about whether tokens are enabled."""
        api.reply("GET", TOKENS, 404, json={"detail": "Not Found"})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 1
        assert "HTTP 404" in result.stderr
        assert "not enabled" not in result.stderr

    def test_invalid_cursor_from_the_server_is_an_error(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [_token()], "pagination": {"next_cursor": "stale"}})
        api.reply("GET", TOKENS, 400, json={"detail": "Invalid cursor"})
        result = runner.invoke(cli, ["token", "list"])
        assert result.exit_code == 1
        assert "Invalid cursor" in result.stderr


class TestTokenView:
    def test_shows_the_token_and_its_grants(self, runner, api):
        token = _token(grants=[REPO_GRANT, VM_GRANT], allow_vm_create=True, vm_create_limit=3, vm_create_count=1)
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=token)
        result = runner.invoke(cli, ["token", "view", TOKEN_ID])
        assert result.exit_code == 0, result.output
        for expected in (
            TOKEN_ID,
            "ci-read",
            "usr-1",
            "2099-01-01",
            "active",
            "rep-aaa",
            "read",
            "cvm-1",
            "admin",
        ):
            assert expected in result.stdout
        assert "1 of 3" in result.stdout

    def test_grant_on_every_vm(self, runner, api):
        grant = {"resource_type": "customer_vm", "resource_id": None, "access_level": "admin"}
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=_token(grants=[grant]))
        result = runner.invoke(cli, ["token", "view", TOKEN_ID])
        assert result.exit_code == 0, result.output
        assert "every VM" in result.stdout
        assert "None" not in result.stdout

    def test_revoked_token(self, runner, api):
        token = _token(revoked_at="2026-10-07T11:00:00Z", revoked_reason="user_revoked")
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=token)
        result = runner.invoke(cli, ["token", "view", TOKEN_ID])
        assert result.exit_code == 0, result.output
        assert "revoked" in result.stdout
        assert "user_revoked" in result.stdout
        assert "active" not in result.stdout

    def test_expired_token(self, runner, api):
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=_token(expires_at="2020-01-01T00:00:00Z"))
        result = runner.invoke(cli, ["token", "view", TOKEN_ID])
        assert result.exit_code == 0, result.output
        assert "expired" in result.stdout
        assert "active" not in result.stdout

    def test_json(self, runner, api):
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=_token())
        result = runner.invoke(cli, ["token", "view", TOKEN_ID, "--json", "*"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == _token()

    def test_org_option(self, runner, api):
        api.reply(
            "GET",
            f"/orgs/org-other/access-tokens/{TOKEN_ID}",
            json=_token(organization_id="org-other"),
        )
        result = runner.invoke(cli, ["token", "view", TOKEN_ID, "--org", "org-other"])
        assert result.exit_code == 0, result.output

    def test_404_points_at_the_list(self, runner, api):
        api.reply("GET", f"{TOKENS}/{UNKNOWN_TOKEN_ID}", 404, json={"detail": "Not found"})
        result = runner.invoke(cli, ["token", "view", UNKNOWN_TOKEN_ID])
        assert result.exit_code == 1
        assert "HTTP 404" in result.stderr
        assert "avr token list" in result.stderr

    def test_token_id_is_required(self, runner, api):
        result = runner.invoke(cli, ["token", "view"])
        assert result.exit_code == 2
        assert api.requests == []


class TestTokenRevoke:
    def test_yes_skips_the_prompt(self, runner, api):
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID, "--yes"])
        assert result.exit_code == 0, result.output
        assert len(api.sent("DELETE", f"{TOKENS}/{TOKEN_ID}")) == 1
        assert TOKEN_ID in result.stdout
        assert "revoked" in result.stdout

    def test_revoking_twice_succeeds(self, runner, api):
        # The API answers 204 for an already-revoked token as well.
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        first = runner.invoke(cli, ["token", "revoke", TOKEN_ID, "-y"])
        second = runner.invoke(cli, ["token", "revoke", TOKEN_ID, "-y"])
        assert first.exit_code == 0, first.output
        assert second.exit_code == 0, second.output
        assert second.stdout == first.stdout
        assert len(api.sent("DELETE", f"{TOKENS}/{TOKEN_ID}")) == 2

    def test_confirmed_at_the_prompt(self, runner, api):
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID], input="y\n")
        assert result.exit_code == 0, result.output
        assert len(api.sent("DELETE", f"{TOKENS}/{TOKEN_ID}")) == 1

    def test_declined_at_the_prompt(self, runner, api):
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID], input="n\n")
        assert result.exit_code != 0
        assert api.requests == []

    def test_refuses_to_prompt_when_prompts_are_disabled(self, runner, api, monkeypatch):
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID])
        assert result.exit_code != 0
        assert "AVR_PROMPT_DISABLED" in result.stderr
        assert "--yes" in result.stderr
        assert api.requests == []

    def test_yes_works_when_prompts_are_disabled(self, runner, api, monkeypatch):
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID, "--yes"])
        assert result.exit_code == 0, result.output

    def test_org_option(self, runner, api):
        api.reply("DELETE", f"/orgs/org-other/access-tokens/{TOKEN_ID}", 204)
        result = runner.invoke(cli, ["token", "revoke", TOKEN_ID, "--org", "org-other", "--yes"])
        assert result.exit_code == 0, result.output

    def test_404_points_at_the_list(self, runner, api):
        api.reply("DELETE", f"{TOKENS}/{UNKNOWN_TOKEN_ID}", 404, json={"detail": "Not found"})
        result = runner.invoke(cli, ["token", "revoke", UNKNOWN_TOKEN_ID, "--yes"])
        assert result.exit_code == 1
        assert "HTTP 404" in result.stderr
        assert "avr token list" in result.stderr


class TestNotGatedOnTheFeature:
    """Tokens can always be found and revoked, including where creating them
    is not enabled: these commands send their one request and nothing else."""

    def test_list(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [_token()], "pagination": {"next_cursor": None}})
        assert runner.invoke(cli, ["token", "list"]).exit_code == 0
        assert [f"{r.method} {r.url.path}" for r in api.requests] == [f"GET {TOKENS}"]

    def test_view(self, runner, api):
        api.reply("GET", f"{TOKENS}/{TOKEN_ID}", json=_token())
        assert runner.invoke(cli, ["token", "view", TOKEN_ID]).exit_code == 0
        assert [f"{r.method} {r.url.path}" for r in api.requests] == [f"GET {TOKENS}/{TOKEN_ID}"]

    def test_revoke(self, runner, api):
        api.reply("DELETE", f"{TOKENS}/{TOKEN_ID}", 204)
        assert runner.invoke(cli, ["token", "revoke", TOKEN_ID, "--yes"]).exit_code == 0
        assert [f"{r.method} {r.url.path}" for r in api.requests] == [f"DELETE {TOKENS}/{TOKEN_ID}"]


class TestTokenGroup:
    def test_help_lists_the_four_commands(self, runner):
        result = runner.invoke(cli, ["token", "--help"])
        assert result.exit_code == 0
        for name in ("create:", "list:", "revoke:", "view:"):
            assert name in result.output

    def test_top_level_help_lists_the_group(self, runner):
        result = runner.invoke(cli, ["--help"])
        assert result.exit_code == 0
        assert "token:" in result.output

    def test_plural_alias(self, runner, api):
        api.reply("GET", TOKENS, json={"data": [], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["tokens", "list"])
        assert result.exit_code == 0, result.output

    def test_reference_docs_cover_the_group(self):
        tree = build_tree()
        (group,) = [node for nodes in tree["sections"].values() for node in nodes if node["name"] == "token"]
        assert [node["name"] for node in group["subcommands"]] == ["create", "list", "revoke", "view"]
        assert tree["aliases"]["tokens"] == "token"
