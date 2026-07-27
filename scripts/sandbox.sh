#!/usr/bin/env bash
# Build a throwaway repo that exercises everything Drift ships today:
# targets + fan-out, ahead/behind, dirty, unmergeable collisions from BOTH
# detection halves, a working-tree-only collision, and a branch that is behind
# with nothing to reconcile (mergeable changes must never be surfaced).
#
#   scripts/sandbox.sh [SANDBOX_DIR] [--wizard]
#
# --wizard leaves the repo unconfigured so the first-run wizard opens instead of
# the dashboard. Re-run without it to get the full config back.
set -euo pipefail

SANDBOX="${1:-$HOME/dev/repos/drift-sandbox}"
[[ "${1:-}" == --* ]] && SANDBOX="$HOME/dev/repos/drift-sandbox"
# The repo this script lives in, so it works from any working directory.
DRIFT_SRC="${DRIFT_SRC:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
WIZARD=false
for arg in "$@"; do [[ "$arg" == "--wizard" ]] && WIZARD=true; done

REPO="$SANDBOX/repo"
ORIGIN="$SANDBOX/origin.git"

# This wipes and rebuilds SANDBOX, so refuse anything that isn't already one of
# ours — a mistyped path must not turn into an rm -rf of real work.
if [[ -e "$SANDBOX" && ! -d "$SANDBOX/origin.git" ]]; then
  echo "refusing to wipe $SANDBOX — it exists and is not a drift sandbox" >&2
  exit 1
fi

rm -rf "$SANDBOX"
mkdir -p "$SANDBOX"

git init --quiet --bare --initial-branch=main "$ORIGIN"
git init --quiet --initial-branch=main "$REPO"
cd "$REPO"
git config user.email "you@example.com"
git config user.name "Sandbox"
git remote add origin "$ORIGIN"

commit() { git add -A && git commit --quiet -m "$1"; }

# --- base: what every branch diverges from ----------------------------------

mkdir -p workflows/onboarding workflows/checkout Assets/Scenes App.xcodeproj src

# The pbxproj class is declared to git itself, so detection's check-attr half is
# live from the start — and declaring it again reports "already declared".
cat > .gitattributes <<'EOF'
# team rules
*.png binary
*.pbxproj -merge
EOF

cat > workflows/onboarding/flow.uwe <<'EOF'
<workflow name="onboarding" version="4">
  <step id="1" label="scan badge"/>
  <step id="2" label="confirm identity"/>
  <script>function next(ctx) { return ctx.step + 1; }</script>
</workflow>
EOF

cat > workflows/checkout/pay.uwe <<'EOF'
<workflow name="checkout" version="2">
  <step id="1" label="scan item"/>
  <step id="2" label="take payment"/>
  <script>function total(cart) { return cart.sum(); }</script>
</workflow>
EOF

cat > Assets/Scenes/Main.unity <<'EOF'
%YAML 1.1
--- !u!1 &100000
GameObject:
  m_Name: Player
  m_Layer: 0
EOF

cat > App.xcodeproj/project.pbxproj <<'EOF'
// !$*UTF8*$!
{
  objects = {
    AA01 /* App */ = { isa = PBXGroup; name = App; };
  };
}
EOF

printf 'package main\n\nfunc main() { println("v1") }\n' > src/app.go
printf '# Sandbox\n\nA throwaway repo for exercising drift.\n' > README.md
commit "base"
git push --quiet -u origin main

# --- feature branches, all cut from base ------------------------------------

git branch ABC-101-main
git branch ABC-101-r2perf
git branch ABC-202-main

# --- target: release-to-performance moves ahead ------------------------------

git checkout --quiet -b release-to-performance
sed -i '' 's/m_Name: Player/m_Name: PlayerRig/' Assets/Scenes/Main.unity
commit "r2perf: rename the player object"
sed -i '' 's|AA01 /\* App \*/ = { isa = PBXGroup; name = App; };|AA01 /* App */ = { isa = PBXGroup; name = App; };\n    BB02 /* Perf */ = { isa = PBXGroup; name = Perf; };|' App.xcodeproj/project.pbxproj
commit "r2perf: add the perf group"
printf 'package main\n\nfunc main() { println("v1 (perf)") }\n' > src/app.go
commit "r2perf: tune the entry point"
git push --quiet -u origin release-to-performance

# --- target: main moves ahead ------------------------------------------------

git checkout --quiet main
sed -i '' 's|<step id="2" label="confirm identity"/>|<step id="2" label="confirm identity"/>\n  <step id="3" label="capture signature"/>|' workflows/onboarding/flow.uwe
commit "main: add the signature step to onboarding"
sed -i '' 's|<step id="2" label="take payment"/>|<step id="2" label="take payment"/>\n  <step id="3" label="print receipt"/>|' workflows/checkout/pay.uwe
commit "main: print a receipt after payment"
sed -i '' 's/m_Layer: 0/m_Layer: 5/' Assets/Scenes/Main.unity
commit "main: move the player to layer 5"
printf 'package main\n\nfunc main() { println("v2") }\n' > src/app.go
printf '# Sandbox\n\nA throwaway repo for exercising drift. Now with more history.\n' > README.md
commit "main: bump the version and the readme"
git push --quiet origin main

# --- branch side: what each feature branch changed ---------------------------

# ABC-101-main: one committed unmergeable collision (flow.uwe) plus a mergeable
# one (app.go) that must never be surfaced.
git checkout --quiet ABC-101-main
sed -i '' 's|<step id="1" label="scan badge"/>|<step id="1" label="scan badge or QR"/>|' workflows/onboarding/flow.uwe
commit "ABC-101: accept a QR code at the badge step"
printf 'package main\n\nfunc main() { println("v1 + ABC-101") }\n' > src/app.go
commit "ABC-101: log the build variant"

# ABC-101-r2perf: collisions from BOTH detection halves — the .unity via a
# config glob, the .pbxproj via the committed .gitattributes.
git checkout --quiet ABC-101-r2perf
sed -i '' 's/m_Name: Player/m_Name: PlayerAvatar/' Assets/Scenes/Main.unity
commit "ABC-101: rename the player for the perf build"
sed -i '' 's|name = App; };|name = App; };\n    CC03 /* ABC101 */ = { isa = PBXGroup; name = ABC101; };|' App.xcodeproj/project.pbxproj
commit "ABC-101: add the feature group"

# ABC-202-main: behind, but everything it touched merges cleanly. Its branch row
# must show no unmergeable marker at all.
git checkout --quiet ABC-202-main
printf 'package main\n\nfunc main() { println("v1 + ABC-202") }\n' > src/app.go
commit "ABC-202: adjust the entry point"

# ABC-303-main: cut from the current origin/main, so it sits at ↓0 ↑0.
git branch ABC-303-main origin/main

# --- working tree: the uncommitted half of the branch side -------------------
# main added a receipt step to pay.uwe; this edit collides with it and exists
# only in the working tree — exactly what a plain `git stash` would capture.

git checkout --quiet ABC-101-main
sed -i '' 's|<step id="2" label="take payment"/>|<step id="2" label="take payment or split bill"/>|' workflows/checkout/pay.uwe

# --- drift config ------------------------------------------------------------

if [[ "$WIZARD" == false ]]; then
  mkdir -p .git/drift
  cat > .git/drift/config.json <<'EOF'
{
  "targets": [
    { "key": "main", "ref": "origin/main" },
    { "key": "r2perf", "ref": "origin/release-to-performance" }
  ],
  "unmergeable": [
    { "name": "workflows", "globs": ["workflows/**/*.uwe"] },
    { "name": "unity", "globs": ["**/*.unity", "**/*.prefab"] }
  ],
  "declare": {
    "destinations": ["local"]
  }
}
EOF
fi

# --- the binary --------------------------------------------------------------

(cd "$DRIFT_SRC" && go build -o "$SANDBOX/drift" .)

cat <<EOF

  sandbox ready:  $REPO
  run it:         cd $REPO && ../drift

  on branch ABC-101-main, with one uncommitted edit (workflows/checkout/pay.uwe)
EOF
if [[ "$WIZARD" == true ]]; then
  echo "  unconfigured — the first-run wizard opens"
else
  echo "  configured — targets main + r2perf, unmergeable globs for .uwe and .unity"
fi
echo
