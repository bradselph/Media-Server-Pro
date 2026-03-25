# Install the framework into an **existing** project

The wizard **`configure_synthesis.py`** only configures paths and generates **`SYNTHESIS_PROJECT.md`**. It does **not** copy files from GitHub into your repo.

Use **`install_framework.py`** once to drop the framework into **your** repository root.

## Prerequisites

- Python 3.11+ on your machine.
- Your project is already a **git repo** (recommended).

## Steps

### 1. Clone this framework (temporary)

```bash
git clone https://github.com/bradselph/agents-workflow.git /tmp/synthesis-framework
cd /tmp/synthesis-framework
git checkout cursor/constrained-synthesis-framework-e32a   # or main, if merged
```

(Use whatever branch you trust.)

### 2. Install into your project root

```bash
python3 /tmp/synthesis-framework/scripts/install_framework.py --target /absolute/path/to/your/project
```

Preview without writing:

```bash
python3 /tmp/synthesis-framework/scripts/install_framework.py --target /path/to/your/project --dry-run
```

If you already ran install once and only want **new** files (never overwrite):

```bash
python3 /tmp/synthesis-framework/scripts/install_framework.py --target /path/to/your/project --merge
```

### 3. Configure for your layout

```bash
cd /path/to/your/project
python3 scripts/configure_synthesis.py
```

Answer prompts for **your** `api_spec` (or equivalent), **backend**, and **frontend** directories. This writes **`synthesis/project_settings.json`**, syncs **`synthesis/partitions.json`**, and **`SYNTHESIS_PROJECT.md`**.

### 4. Commit

```bash
git add .cursor scripts synthesis docs .github CLAUDE.md CONTRIBUTING.md SYNTHESIS_PROJECT.md
git status   # review
git commit -m "Add constrained synthesis framework"
```

## What gets installed

| Copied | Purpose |
|--------|---------|
| `.cursor/rules/` | Cursor agent rules |
| `scripts/` | Partition check, validate, wizard, **install_framework.py**, smoke |
| `docs/` | Architecture, workflows, prompts, scenarios |
| `synthesis/partitions.json`, `*.schema.json`, `project_settings.template.json` | Manifest + template |
| `synthesis/project_settings.json` | From template if missing (`initialized: false` until you run wizard) |
| `.github/workflows/synthesis-ci.yml` | CI |
| `CLAUDE.md`, `CONTRIBUTING.md` | Claude / contributors |

**Not** copied (your code stays yours): `api_spec/`, `server/`, `client/`, `examples/`. Add OpenAPI and partitions yourself or copy from the framework repo manually if you want the demo stack.

## If `configure_synthesis.py` creates only `project_settings.json`

That happens when someone copied **only** the wizard. You still need **`synthesis/partitions.json`** and the rest — run **`install_framework.py`** as above.

## Re-install after framework updates

```bash
python3 /tmp/synthesis-framework/scripts/install_framework.py --target /path/to/your/project --merge
```

Use **`--force-settings`** only if you intend to reset **`project_settings.json`** from the template (you will lose local wizard answers unless backed up).
