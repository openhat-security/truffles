# truffles — combine (secret-dedup) + clean rebuild on fixtures.
#
#   make demo-vhs TAPE=results
#   vhs assets/results.tape

Output assets/screenshots/results.gif
Output assets/screenshots/results.mp4

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
Type "rm -f /tmp/demo-combined.csv"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "ls assets/fixtures/demo-results*.csv"
Sleep 1.5s
Enter
Sleep 3.5s

Type "./truffles combine -o /tmp/demo-combined.csv assets/fixtures/demo-results.csv assets/fixtures/demo-results-b.csv"
Sleep 2.2s
Enter
Sleep 6s

Type "wc -l /tmp/demo-combined.csv && cat /tmp/demo-combined.csv"
Sleep 1.5s
Enter
Sleep 6s

Type "./truffles consolidate -h"
Sleep 1.5s
Enter
Sleep 6s

Type "echo '✓ combine dedupes by secret; consolidate rolls up data/'"
Sleep 1.8s
Enter
Sleep 4s
