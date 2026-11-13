# Schema examples: proposed change-workflow contract

**Proposed contract, not implemented.** These are focused positive and
negative instances for the owner re-review residuals (R1, R2, R4, R5) of
ADR-0008/0009. They make the schema rules reviewable. Each file is one
instance:

| Field | Meaning |
|---|---|
| `finding` | The re-review residual it covers |
| `acceptance` | The acceptance scenario in ADR-0008 §5 or ADR-0009 §2 it supports (`—` if none) |
| `schema` | The schema in `../change-workflow.yaml`, as a path under `#/components/schemas/` |
| `expect` | `valid` or `invalid`; the file sits in the folder of the same name |
| `why` | The rule the instance demonstrates |
| `instance` | The JSON instance |

## What the schema can and cannot show

**The schema enforces structure.** It checks:
- the typed supersession entries and their expected revisions
- the services with their ports, and the destination prefixes
- that each kind of configuration evidence carries its required fields
- that every known admission or physical-dependency value carries a basis-specific evidence reference and validity: exclusive admission never rests on a session count, a sessions lower bound needs an observation reference and at least 2 distinct clients, and `no_dependency` never rests on an admission setting
- that ports are always explicit
- the execution's authority state and stop reasons

**Semantic rules are acceptance scenarios, not schema checks.** That covers:
- port and prefix **containment**
- topology and authentication **matching**
- **expiry against the clock**
- the **Stop/activation race**

They are listed as `A-R1-*`, `A-R2-*` and `A-R5-*` in ADR-0008 §5, and as
`A-R4-*` in ADR-0009 §2. An implementation needs tests for each.

## Rerun the check

With Python 3, `PyYAML` and `jsonschema` (Draft 2020-12, `referencing`),
from the repository root:

```python
import glob, json, os, yaml
from jsonschema import Draft202012Validator
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

P = "https://nac.local/api/openapi/proposed/change-workflow.yaml"
B = "https://nac.local/api/openapi/openapi.yaml"
reg = Registry().with_resources([
    (P, Resource.from_contents(yaml.safe_load(open("api/openapi/proposed/change-workflow.yaml")), DRAFT202012)),
    (B, Resource.from_contents(yaml.safe_load(open("api/openapi/openapi.yaml")), DRAFT202012)),
])
for f in sorted(glob.glob("api/openapi/proposed/examples/*/*.json")):
    d = json.load(open(f))
    v = Draft202012Validator({"$ref": P + "#/components/schemas/" + d["schema"]}, registry=reg)
    ok = (not list(v.iter_errors(d["instance"]))) == (d["expect"] == "valid")
    print("OK " if ok else "BAD", d["expect"], os.path.basename(f))
```

All 52 instances behave as labelled (2026-11-12, `jsonschema` 4.26), also with `format_checker=Draft202012Validator.FORMAT_CHECKER`.
