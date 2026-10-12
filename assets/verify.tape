# truffles — live-verify secrets from a CSV (fake fixture keys).
#
#   make demo-vhs TAPE=verify
#   vhs assets/verify.tape

Output assets/screenshots/verify.gif
Output assets/screenshots/verify.mp4

Require go
Require ffmpeg
Require trufflehog

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 42ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 300ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "rm -f /tmp/demo-results-verified.csv"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "head -5 assets/fixtures/demo-results.csv"
Sleep 1.5s
Enter
Sleep 5s

Type "./truffles verify -f assets/fixtures/demo-results.csv -w 2 -out /tmp/demo-results-verified.csv"
Sleep 2s
Enter
Sleep 8s

Type "head -5 /tmp/demo-results-verified.csv"
Sleep 1.5s
Enter
Sleep 5s

Type "echo '✓ verify done — no git clone needed'"
Sleep 1.5s
Enter
Sleep 3.5s
