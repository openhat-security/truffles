# truffles — search a filtered list, then scan with trufflehog.
#
#   make demo-vhs TAPE=scan
#   vhs assets/scan.tape

Output assets/screenshots/scan.gif
Output assets/screenshots/scan.mp4

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
Type "rm -f repos-run.txt /tmp/truffles-scan-demo /tmp/truffles-scan-demo.csv"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles search -no-proxy -owner openhat-security '*run*' -out repos-run.txt"
Sleep 2s
Enter
Sleep 7s

Type "cat repos-run.txt"
Sleep 1.5s
Enter
Sleep 3.5s

Type "./truffles scan -f repos-run.txt -w 2 -format pretty -since 1y -out /tmp/truffles-scan-demo -exclude-paths 'node_modules,vendor,*.lock'"
Sleep 2.5s
Enter
Sleep 18s

Type "echo '✓ scan complete — report at /tmp/truffles-scan-demo(.csv)'"
Sleep 1.5s
Enter
Sleep 4s
