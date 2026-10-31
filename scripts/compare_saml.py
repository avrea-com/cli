"""SAML parity with local XML fixtures and captured browser invocations."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import urlencode
import sys

ORG = inspection.ORG
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
BASE = f"/orgs/{ORG}/saml"
XML = '<?xml version="1.0" encoding="UTF-8"?>\n<EntityDescriptor entityID="https://idp.example.test/équipe"/>\n'
ROW = {
    "organization_saml_config_id": "osc-019a0000000070008000000000000004",
    "organization_id": ORG,
    "idp_entity_id": "https://idp.example.test/entity",
    "idp_sso_url": "https://idp.example.test/sso",
    "idp_slo_url": None,
    "name_id_format": "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
    "attr_email": "email",
    "attr_given_name": None,
    "attr_family_name": None,
    "attr_groups": None,
    "is_enforced": False,
    "jit_provisioning": True,
    "allow_idp_initiated": False,
    "default_role": "user",
    "group_sync_state": "attr_groups_unset",
    "group_grant_stale_after_days": 30,
    "created_at": "2026-10-01T12:00:00Z",
    "updated_at": "2026-10-02T15:34:56.789123+03:00",
}
DEFAULT = {"attr_email": "email", "default_role": "user", "jit_provisioning": "true", "allow_idp_initiated": "false"}
POST = ("POST", BASE + "?" + urlencode(DEFAULT))


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {"name": "SAML " + name, "args": ["org", "saml", *args], "env": ENV.copy(), "stdin": XML, **kwargs}
        )

    for label, args, route in [
        ("show", ["show"], ("GET", BASE)),
        ("configure", ["configure", "-"], POST),
        ("enforcement", ["enforcement", "on"], ("POST", BASE + "/enforcement")),
    ]:
        responses = {route: (200, {"data": ROW})}
        case(label + " human", args, responses=responses, presentation=True)
        case(label + " JSON", args + ["--json", "*"], responses=responses, json=True)
        case(label + " jq", args + ["--json", "idp_entity_id", "-q", ".idp_entity_id"], responses=responses)
        case(label + " empty fields", args + ["--json", ""], responses=responses, json=True)
        case(label + " discovery", args + ["--json", "?"], env={}, responses={})
        case(
            label + " unknown field",
            args + ["--json", "wrong"],
            responses={},
            exit=1,
            error_terms=["Unknown JSON field"],
        )
        case(
            label + " jq without json", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
        case(label + " no auth", args, env={}, responses={}, exit=4)
        case(label + " org slug", args + ["--org", "AcMe"], responses=responses | ORGS, presentation=True)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                label + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    full = ROW | {
        "idp_slo_url": "https://idp.example.test/slo",
        "attr_given_name": "first",
        "attr_family_name": "last",
        "attr_groups": "groups",
        "is_enforced": True,
        "jit_provisioning": False,
        "allow_idp_initiated": True,
        "default_role": "admin",
    }
    case("show full", ["show"], responses={("GET", BASE): (200, {"data": full})}, presentation=True)
    for name, content in [("file", XML.encode()), ("empty file", b""), ("opaque file", b"\xff\x00<metadata/>")]:
        case(
            "configure " + name,
            ["configure", "FIXTURE_HOME/idp.xml"],
            fixture_files={"idp.xml": content},
            responses={POST: (200, {"data": ROW})},
            presentation=True,
        )
    case(
        "configure missing file",
        ["configure", "missing-idp.xml"],
        responses={},
        exit=2,
        error_terms=["METADATA", "No such file"],
    )
    case(
        "configure discovery missing file",
        ["configure", "missing-idp.xml", "--json", "?"],
        responses={},
        exit=2,
        error_terms=["METADATA", "No such file"],
    )
    for aliases in [False, True]:
        flags = (
            ["--attr-email", "--attr-given-name", "--attr-family-name", "--attr-groups"]
            if aliases
            else ["--email-attribute", "--given-name-attribute", "--family-name-attribute", "--groups-attribute"]
        )
        args = ["configure", "-"]
        for flag, value in zip(flags, ["mail", "first", "last", "groups"], strict=True):
            args += [flag, value]
        args += ["--default-role", "BILLING_ADMIN", "--no-jit-provisioning", "--allow-idp-initiated"]
        query = DEFAULT | {
            "attr_email": "mail",
            "attr_given_name": "first",
            "attr_family_name": "last",
            "attr_groups": "groups",
            "default_role": "billing_admin",
            "jit_provisioning": "false",
            "allow_idp_initiated": "true",
        }
        case(
            "configure all options " + str(aliases),
            args,
            responses={("POST", BASE + "?" + urlencode(query)): (200, {"data": full})},
            presentation=True,
        )
    for flags, jit, idp in [
        (["--no-jit-provisioning", "--jit-provisioning"], "true", "false"),
        (["--jit-provisioning", "--no-jit-provisioning"], "false", "false"),
        (["--allow-idp-initiated", "--no-allow-idp-initiated"], "true", "false"),
        (["--no-allow-idp-initiated", "--allow-idp-initiated"], "true", "true"),
    ]:
        query = DEFAULT | {"jit_provisioning": jit, "allow_idp_initiated": idp}
        case(
            "configure toggles " + " ".join(flags),
            ["configure", "-", *flags],
            responses={("POST", BASE + "?" + urlencode(query)): (200, {"data": ROW})},
            presentation=True,
        )
    case(
        "configure last alias",
        ["configure", "-", "--email-attribute", "first", "--attr-email", "last"],
        responses={("POST", BASE + "?" + urlencode(DEFAULT | {"attr_email": "last"})): (200, {"data": ROW})},
    )
    case(
        "configure omitted empty attributes",
        ["configure", "-", "--given-name-attribute", "", "--family-name-attribute", "", "--groups-attribute", ""],
        responses={POST: (200, {"data": ROW})},
    )
    case(
        "configure invalid role",
        ["configure", "-", "--default-role", "owner"],
        responses={},
        exit=2,
        error_terms=["--default-role"],
    )
    for state in ["ON", "off", "OFF"]:
        case(
            "enforcement " + state,
            ["enforcement", state],
            responses={("POST", BASE + "/enforcement"): (200, {"data": ROW | {"is_enforced": state.lower() == "on"}})},
        )
    case("enforcement invalid", ["enforcement", "yes"], responses={}, exit=2, error_terms=["{on|off}"])
    for name, body in [("with newline", XML.encode()), ("without newline", XML.rstrip().encode()), ("empty", b"")]:
        case(
            "metadata " + name,
            ["sp-metadata"],
            responses=ORGS
            | {("GET", "/saml/acme/metadata"): (200, body, {"Content-Type": "application/samlmetadata+xml"})},
        )
    case(
        "metadata org slug",
        ["sp-metadata", "--org", "acme"],
        responses=ORGS | {("GET", "/saml/acme/metadata"): (200, XML.encode())},
    )
    for status in [401, 403, 404, 503]:
        case(
            "metadata HTTP " + str(status),
            ["sp-metadata"],
            responses=ORGS | {("GET", "/saml/acme/metadata"): (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    case("test no browser", ["test", "--no-browser"], responses=ORGS)
    case("test browser", ["test"], responses=ORGS, browser_capture=True)
    case("test org slug", ["test", "--org", "AcMe", "--no-browser"], responses=ORGS)
    for verb in ["sp-metadata", "test"]:
        case(verb + " no auth", [verb], env={}, responses={}, exit=4)
        case(
            verb + " unknown slug", [verb], responses={("GET", "/users/me/organizations"): (200, {"data": []})}, exit=1
        )
    delete = {("DELETE", BASE): (200, {"data": {"deleted": True}})}
    case("remove yes", ["remove", "--yes"], responses=delete)
    case("remove short yes", ["remove", "-y"], responses=delete)
    case("remove confirm", ["remove"], stdin="y\n", responses=delete, presentation=True)
    case("remove decline", ["remove"], stdin="n\n", responses={}, exit=1, presentation=True)
    case("remove EOF", ["remove"], stdin="", responses={}, exit=1, presentation=True)
    case("remove disabled", ["remove"], env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    case("remove no auth", ["remove", "--yes"], env={}, responses={}, exit=4)
    case("remove org slug", ["remove", "--yes", "--org", "acme"], responses=delete | ORGS)
    for status in [401, 403, 404, 409, 422, 503]:
        case(
            "remove HTTP " + str(status),
            ["remove", "--yes"],
            responses={("DELETE", BASE): (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for args, term in [(["configure"], "METADATA"), (["enforcement"], "{on|off}")]:
        case("arguments " + args[0], args, responses={}, exit=2, error_terms=[term])
    if sys.platform != "win32":
        for name in [
            "show human",
            "show full",
            "configure human",
            "enforcement human",
            "metadata with newline",
            "test no browser",
            "remove confirm",
            "remove decline",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "SAML " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=watch.compare, default_report="build/go/saml-parity.json")
    )
