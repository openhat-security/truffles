# truffles job — list / status / --full / logs against the live remote loop.
# Does not start or stop the running job.
#
#   make demo-vhs TAPE=job
#   vhs assets/job.tape

Output assets/screenshots/job.gif
Output assets/screenshots/job.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 14
Set Width 1400
Set Height 900
Set TypingSpeed 40ms
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
Type "test -f remote.env || { echo 'missing remote.env'; exit 1; }"
Enter
Sleep 800ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles job list"
Sleep 1.5s
Enter
Sleep 5s

Type "./truffles job status --color always"
Sleep 1.8s
Enter
Sleep 12s

Type "./truffles job status --full --color always"
Sleep 2s
Enter
Sleep 14s

Type "./truffles job logs $(ls -1 data/jobs | head -1) -n 25"
Sleep 2s
Enter
Sleep 10s

Type "echo '✓ job status shows repos/hr, disk, workers — tip: --full for cost'"
Sleep 2s
Enter
Sleep 4.5s
