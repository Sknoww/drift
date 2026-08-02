# VHS tape for the README hero GIF.
#   brew install vhs
#   scripts/sandbox.sh                 # build the throwaway repo it records against
#   vhs scripts/demo.tape              # writes docs/demo.gif
#
# Keep this checked in: the GIF is regenerable, so it never goes stale silently.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1180
Set Height 640
Set Padding 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set PlaybackSpeed 1.0

Hide
Type "cd ~/dev/repos/drift-sandbox/repo && clear" Enter
Sleep 800ms
Show

# --- the dashboard: three tickets, fanning out across long-lived mains ---
# ../drift is the sandbox-built binary, per the README's sandbox section — never
# the one on PATH, which may be an older release.
Type "../drift"
Sleep 400ms
Enter
Sleep 3s

# expand the first ticket -> one branch per target, each with its own drift
Enter
Sleep 3s

# walk the branches; the unmergeable marker is the thing to notice
Down
Sleep 1600ms
Down
Sleep 2200ms

# open the diff panel on a branch with collisions
Up
Sleep 600ms
Enter
Sleep 3s

# cycle the colliding files
Tab
Sleep 2200ms
Tab
Sleep 2200ms

Escape
Sleep 1800ms

# the targets view: what each long-lived main actually points at
Type "t"
Sleep 2800ms
Escape
Sleep 1200ms

Type "q"
Sleep 1500ms
