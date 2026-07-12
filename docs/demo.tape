# vhs tape for the sshush demo GIF.
#
# Generate it with:
#   sh docs/demo-fixture.sh    # throwaway keys/hosts/agent, then vhs docs/demo.tape
#
# Install vhs: https://github.com/charmbracelet/vhs (`brew install vhs`).
# The fixture keeps real key names and hosts out of the recording, and starts
# on the "tokyonight" theme (the switch below lands on tokyonight-day with two
# presses).

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1120
Set Height 560
Set Padding 20
Set Theme "TokyoNight"
Set TypingSpeed 110ms

# Launch (hide the typing of the command itself).
Hide
Type "sshush"
Enter
Sleep 1.5s
Show

# Keys pane — hold so the loaded badges, default star, and host tags are legible.
Sleep 2.5s
Down
Sleep 1300ms
Down
Sleep 1300ms
Down
Sleep 1500ms

# Hosts pane — browse destinations and the read-only Match blocks.
Tab
Sleep 2s
Down
Sleep 1100ms
Down
Sleep 1100ms
Down
Sleep 1100ms
Down
Sleep 1300ms
Down
Sleep 1500ms

# Filter the active pane.
Type "/"
Sleep 900ms
Type "prod"
Sleep 2s
Escape
Sleep 1300ms

# Back to the Keys pane for the theme demo.
Tab
Sleep 1300ms

# Live theme switch: tokyonight -> tokyonight-day (dark to light).
Type "t"
Sleep 1800ms
Down
Sleep 1200ms
Down
Sleep 1500ms
Enter
Sleep 2800ms

# Full keybinding help — hold long enough to read.
Type "?"
Sleep 3200ms
Escape
Sleep 1300ms

# Quit.
Type "q"
Sleep 1300ms
