# artifactory-cleaner

A Go CLI tool for cleaning up stale artifacts from JFrog Artifactory repositories.
Supports Docker (multi-platform and single-platform), Maven, and generic repositories.

---

## How it works

The cleaner fetches artifacts via the Artifactory AQL API, groups them by image/module,
applies configurable retention rules per version pattern, and produces a report before
optionally deleting anything.

### Decision flow (per artifact, sorted newest-first within each group)

```
1. protectedGroups  → PROTECTED         (immune, checked before rules)
2. protectedVersions → PROTECTED        (immune, checked before rules)
3. First rule whose pattern matches version:
   a. rule whitelist                           → WHITELISTED
   b. within recentArtifactRetention count     → RECENT_VERSION
   c. within artifactLifetimeDays grace period → CREATED_RECENTLY
   d. within lastDownloadedDays window         → DOWNLOADED_RECENTLY
   e. none of the above                        → DELETE
4. No rule matched → UNMATCHED_KEEP or DELETE (per unmatchedAction)
```

Retention counts are tracked **independently per rule** within each group — snapshots
and releases each have their own counter regardless of interleaving.

### Docker: manifest list awareness

Multi-platform Docker images store a manifest list (`list.manifest.json`) referencing
platform-specific images. The cleaner handles this correctly in two passes.

**Pass 1 — multi-platform (bottom-up decision):**

- Fetches all `list.manifest.json` files and all `manifest.json` checksums in parallel
- Reads each manifest list to get platform image digests (sha256)
- Uses the **platform image `stat.downloaded`** — not the manifest list's own stat —
  as the effective last-pull time. This avoids a contamination problem: reading
  `list.manifest.json` updates its `stat.downloaded`, but `manifest.json` files are
  never read for content by the cleaner, so their stats reflect real `docker pull` events only
- Pattern matching uses the **manifest list tag** (`2.26.0`), not the platform image tag
  (`sha256:abc...` or `amd64-2.26.0`), ensuring rules apply correctly
- Platform images are identified by **content hash** (sha256 field from AQL), covering
  both sha256-addressed directories and named arch tags (`amd64-1.5.1`, `arm64-1.5.1`)
- A manifest list is deleted only when **all** its platform images would be deleted

**Pass 2 — single-platform:**

- Fetches `manifest.json` files (non-sha256 paths) not already handled by Pass 1
- Applies the same retention rules independently

See `docs/docker-cleanup-flow.md` for the detailed architecture with diagrams.

---

## Installation

```bash
go build -o artycleaner .
```

**Requires Go 1.21+**

**Environment variables (required):**

```bash
export ARTIFACTORY_URL=https://your-artifactory.example.com
export ARTIFACTORY_TOKEN=your-api-token
```

---

## Configuration

Create a YAML config file (default: `cleanup_queries.yaml`).
The repository package type (Docker, Maven, etc.) is **auto-detected** from the
Artifactory API — no `discriminator` needed for Docker.

```yaml
cleanup:
  repositories:

    # Docker — one target, two rules, one API fetch
    docker:
      name: docker-local
      unmatchedAction: keep             # keep | delete — for versions matching no rule
      protectedVersions: [latest]       # never deleted, checked before rules
      protectedGroups: [busybox, nginx] # entire image immune to all rules
      concurrency: 8                    # parallel manifest list reads (default: 8)

      rules:
        - name: snapshots
          pattern: ".*-SNAPSHOT.*"
          recentArtifactRetention: 3    # keep 3 newest snapshot versions per image
          lastDownloadedDays: 1         # keep if pulled within 1 day
          artifactLifetimeDays: 7       # grace period: keep anything < 7 days old

        - name: releases
          pattern: "\\d+\\.\\d+(\\.\\d+)*"
          recentArtifactRetention: 5
          lastDownloadedDays: 90
          whitelistedVersions: [1.5.2]      # pin a specific release
          whitelistedArtifacts: [svc@2.0.0] # pin group@version

    # Maven — discriminator defaults to *.pom
    libs-release:
      name: libs-release-local
      unmatchedAction: keep
      rules:
        - name: releases
          pattern: "\\d+\\.\\d+\\.\\d+(?:-RELEASE)?"
          pathMatcher: "*"
          recentArtifactRetention: 5
          lastDownloadedDays: 180

    # Generic — discriminator required
    my-generic:
      name: generic-local
      unmatchedAction: keep
      rules:
        - name: all
          pattern: ".*"
          discriminator: "*.zip"
          pathMatcher: "*"
          recentArtifactRetention: 3
          lastDownloadedDays: 90
```

### Rule fields

| Field | Description | Default |
|-------|-------------|---------|
| `name` | Rule name (shown in reports and validation warnings) | required |
| `pattern` | Regexp matched against the version/tag | required |
| `recentArtifactRetention` | Keep N most recently created matching versions | 0 |
| `lastDownloadedDays` | Keep if last downloaded within N days | 0 |
| `artifactLifetimeDays` | Grace period: keep anything created within N days regardless of downloads | 0 (disabled) |
| `whitelistedVersions` | Versions to keep unconditionally (must match pattern — validated at startup) | [] |
| `whitelistedGroups` | Image/group names to keep within this rule. Supports prefix matching: `org/example/tools` also protects `org/example/tools/submodule`. | [] |
| `whitelistedArtifacts` | `group@version` pairs to keep unconditionally | [] |
| `discriminator` | Filename filter for AQL (Maven/Generic only) | `*.pom` for Maven |
| `pathMatcher` | Path glob for AQL (Maven/Generic only) | `*` |

### Target fields

| Field | Description | Default |
|-------|-------------|---------|
| `name` | Artifactory repository key | required |
| `unmatchedAction` | What to do when no rule pattern matches: `keep` or `delete` | `keep` |
| `protectedVersions` | Tags never deleted, checked before any rule (e.g. `latest`) | [] |
| `protectedGroups` | Image/group names immune to all rules. Also supports prefix matching. | [] |
| `concurrency` | Parallel HTTP requests for manifest list reading (Docker only) | 8 |
| `rules` | Ordered list of retention rules | required |

---

## Usage

```bash
artycleaner [flags]

Flags:
  -t, --target string    Target repository key from the config file (required)
      --config string    Config file path (default: cleanup_queries.yaml)
      --dry-run          Show what would be deleted without deleting anything
      --force            Skip the confirmation prompt
  -f, --format string    Report format: table, csv, xlsx, html (default: table)
  -o, --output string    Write report to file (default: report-{target}.{format})
```

### Examples

```bash
# Dry-run with XLSX report (safe preview, no deletions)
./artycleaner -t docker --dry-run --format xlsx

# Dry-run to stdout
./artycleaner -t docker --dry-run

# Run for real (asks for confirmation)
./artycleaner -t docker --config prod.yaml

# Skip confirmation prompt
./artycleaner -t docker --force
```

---

## Report formats

### Table (terminal)
Colour-coded terminal output. Red = DELETE, green = kept.

### CSV / XLSX
Columns: `Group, Path, Version, Tag, Size, Created At, Last Downloaded At, Cleanup Action`.

The **Tag** column shows the manifest list tag that a platform image belongs to (e.g. `2.26.0`
for a `sha256:abc…` dir or an `amd64-2.26.0` named-arch tag). For manifest list tags themselves
it repeats the version; for standalone images and Maven artifacts it is empty.

XLSX additionally includes a **Summary** sheet with statistics and the action legend.

### Action reference

| Action | Kept? | Reason |
|--------|-------|--------|
| `RECENT_VERSION` | ✓ | Within `recentArtifactRetention` count for its rule |
| `DOWNLOADED_RECENTLY` | ✓ | Platform image downloaded within `lastDownloadedDays` |
| `CREATED_RECENTLY` | ✓ | Created within `artifactLifetimeDays` grace period |
| `WHITELISTED` | ✓ | In the matched rule's whitelist |
| `PROTECTED` | ✓ | In target `protectedVersions` or `protectedGroups` |
| `MANIFEST_LIST_REF` | ✓ | Docker platform image kept because its parent manifest list is retained |
| `UNMATCHED_KEEP` | ✓ | No rule matched; `unmatchedAction` is `keep` |
| `DELETE` | ✗ | None of the above conditions met |

---

## Config validation

On startup the cleaner prints warnings for likely config mistakes (run continues regardless):

- `whitelistedVersions` entry that doesn't match its rule's `pattern` — will never be reached
- `whitelistedArtifacts` version part that doesn't match its rule's `pattern`
- `protectedVersions` entry that also matches a rule pattern — redundant, protection takes priority

```
⚠  Config warnings for target "docker":
   [rule "releases"] whitelistedVersion "latest" does not match pattern "\d+\.\d+\.\d+" — it will never be reached by this rule
```

---

## Architecture

```
cmd/                      CLI entry point (cobra), signal handling
internal/
  client/                 Raw HTTP client (authenticated GET/POST/DELETE, context support)
  artifactory/            Artifactory API wrapper (AQL, manifest reading, repo info, checksum index)
  cleaner/                Domain: retention decisions, plan, report, config loading
  strategy/
    docker/               Docker strategy: multi-platform + single-platform passes
    maven/                Maven strategy: *.pom discriminator default
    generic/              Generic strategy: configurable discriminator
docs/
  docker-cleanup-flow.md  Detailed Docker cleanup architecture with diagrams
```

The cleanup strategy is auto-selected based on `packageType` returned by
`GET /artifactory/api/repositories/{key}`. Only `rclass: local` repositories
can be cleaned — remote and virtual repos are rejected on startup.
