# Autonomy audit v1

## Purpose

`kry autonomy-audit` is a bootstrap-stage audit command. Its purpose is to make
Python ownership and the remaining replacement work observable; it is not a
native compiler, runtime, bundle, or self-hosting claim.

The report is deterministic for a fixed checkout. It does not execute source
programs, resolve packages, access the network, inspect a user's home directory,
or infer independence from the existence of a `.kry` source module.

## Transition stages

| Stage | Meaning | Current status |
| --- | --- | --- |
| stage-0 | Python compiler, VM, CLI, and differential oracle | Present and temporary |
| stage-1 | Kryndel source seams and frozen contracts used for differential work | Present for bounded subsets |
| stage-2 | Native runtime that reads verified artifacts and executes `main` | Not implemented |
| stage-3 | Native compiler and productive CLI | Not implemented |
| stage-4 | Reproducible target-specific user bundle | Not implemented |
| stage-5 | Two equivalent clean native rebuilds | Not implemented |

A `.kry` module interpreted by `kryndel/vm.py` remains a **seam fuente bajo
bootstrap Python**. It must not be labelled native merely because its source is
written in Kryndel.

## Four implementation states

The `states` array and `counts` object in the report use these exact values:

| State | Definition |
| --- | --- |
| `Kryndel-native` | The normal implementation executes as Kryndel-native code and does not enter the Python VM or an unapproved external runtime. |
| `host capability nativa mínima` | The operation is outside the language core and is restricted to a small, explicitly named host capability, with an auditable contract and no Python dependency in that utility's tested path. |
| `bootstrap Python` | The normal implementation or its source seam is executed, loaded, verified, or orchestrated by the Python bootstrap. |
| `no implementado` | The promised replacement or product capability does not yet exist, so no implementation ownership may be inferred. |

The matrix is component-level. It is not a percentage of the whole project and
must not be converted into an autonomy percentage without a separately defined
measurement methodology.

## Report schema

The top-level report has `contract: "kryndel-autonomy-audit"` and `version: 1`.
It contains:

| Field | Meaning |
| --- | --- |
| `bootstrap_modules` | Relative paths of Python modules in `kryndel/`. |
| `normal_python_route` | The documented bootstrap entry point, VM path, and source-seam description. |
| `python_invocations` | Supported repository files and line numbers containing the normal `python -m kryndel` invocation. |
| `status_matrix` | The four states, component records, and deterministic counts. |
| `pending_replacements` | Every matrix component not in `Kryndel-native`. |

Each component record contains a stable `component`, `evidence`, `replacement`, and
`status`. The report scans only UTF-8 Markdown, Python, YAML, and shell files;
`.git` and binary files are excluded. The scan is an inventory, not a proof of
absence of indirect process execution.

## Acceptance

A release or bundle may use this report as an audit input only if the report is
reproducible, the normal Python route is explicit, every component has one of the
four states, and no source seam is presented as native. The formal autonomy gate
remains the stronger requirement in `docs/roadmap-status.md` and includes native
compiler/runtime/CLI ownership, offline installation, clean-machine execution,
reproducible rebuilds, and dependency audits.
