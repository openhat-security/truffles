# truffles — disclose dry-run (private advisory / contact issue drafts).
#
#   make demo-vhs TAPE=disclose
#   vhs assets/disclose.tape

Output assets/screenshots/disclose.gif
Output assets/screenshots/disclose.mp4

Require go
Require ffmpeg

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
Type "rm -rf /tmp/truffles-disclose-demo"
Enter
Sleep 400ms
Type "mkdir -p /tmp/truffles-disclose-demo && cp assets/fixtures/demo-results.csv /tmp/truffles-disclose-demo/results.csv"
Enter
Sleep 500ms
Type "clear"
Enter
Sleep 500ms
Show

Type "head -3 /tmp/truffles-disclose-demo/results.csv"
Sleep 1.5s
Enter
Sleep 4s

Type "./truffles disclose -f /tmp/truffles-disclose-demo/results.csv"
Sleep 2s
Enter
Sleep 8s

Type "echo '✓ dry-run only — add -submit to POST (never in demos)'"
Sleep 1.8s
Enter
Sleep 4s
