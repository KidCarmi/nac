# Contributing

Thanks for your interest in contributing to the NAC platform. The complete
contribution guide — workflow, coding standards, and the **Definition of
Done** — lives at [`docs/guides/contributing.md`](docs/guides/contributing.md).

Quick start:

```bash
make up      # local Postgres + Redis + nacd
make ci      # tidy + fmt + vet + lint + test (must be green before a PR)
```

- Coding standards: [`docs/guides/coding-standards.md`](docs/guides/coding-standards.md)
- Developer guide: [`docs/guides/developer.md`](docs/guides/developer.md)
- Architecture decisions: [`adr/`](adr/README.md) · proposals: [`rfcs/`](rfcs/README.md)
- Security issues: report privately — see [`SECURITY.md`](SECURITY.md).

No change is complete without production code, tests, and synchronized
documentation.
