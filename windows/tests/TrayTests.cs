using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows.Forms;

namespace Sparkle.Windows
{
    internal static class TrayTests
    {
        [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
        [STAThread]
        private static void Main(string[] args)
        {
            string result = Path.Combine(args[0], "test-result.txt");
            try
            {
                Environment.SetEnvironmentVariable("SPARKLE_TEST_DIR", args[0]);
                Program.SetProcessDPIAware();
                Application.EnableVisualStyles();
                string logDirectory = Path.Combine(args[0], ".sparkle-transcoder", "logs");
                string logPath = Path.Combine(logDirectory, "sparkle.log");
                const string previousSession = "Previous tray session belongs in an archive.";
                Directory.CreateDirectory(logDirectory);
                File.WriteAllText(logPath, previousSession);
                var recoveredAt = new DateTime(2001, 2, 3, 4, 5, 6, DateTimeKind.Utc);
                File.SetLastWriteTimeUtc(logPath, recoveredAt);
                DateTime sessionStartedAt = DateTime.UtcNow;
                using (var activation = new EventWaitHandle(false, EventResetMode.AutoReset))
                using (var app = new TrayApplication(args[0], args[1], activation))
                {
                    Wait(delegate { app.LogWindow.RefreshLogs(); return app.LogWindow.LogText.Contains("stderr: ready"); }, "capturing stdout/stderr");
                    Check(!app.LogWindow.LogText.Contains(previousSession), "new tray launch displayed the previous session's logs");
                    string currentLog = ReadLiveLog(logPath);
                    Check(!currentLog.Contains(previousSession) && currentLog.Contains("stderr: ready"), "new tray launch did not replace the previous log file");
                    Check(ReadLiveLog(ArchivePath(logDirectory, recoveredAt, "-recovered")).Contains(previousSession), "unclean session was not recovered before opening the current log");
                    Check(app.LogWindow.LogText.Contains("Unicode \u65e5\u672c\u8a9e"), "UTF-8 output must survive redirection");
                    Check(File.Exists(Path.Combine(args[0], "local-launcher-used.txt")), "tray did not use the local backend launcher");
                    Check(!app.LogWindow.Visible, "startup must be tray-only");
                    Wait(delegate { return File.Exists(Path.Combine(args[0], "child-console.txt")); }, "child startup");
                    Check(File.ReadAllText(Path.Combine(args[0], "backend-console.txt")) == "0", "backend unexpectedly has a console");
                    Check(File.ReadAllText(Path.Combine(args[0], "child-console.txt")) == "0", "child unexpectedly has a console");
                    int launcher = app.BackendProcessId;
                    app.ShowLogs();
                    Check(app.LogWindow.Visible, "logs did not open");
                    Check(IsWindowVisible(app.LogWindow.Handle), "logs were hidden by the process startup window style");
                    app.LogWindow.Close();
                    Check(!app.LogWindow.Visible && !app.LogWindow.IsDisposed, "closing logs must hide the window");
                    Check(app.IsRunning && app.BackendProcessId == launcher, "closing logs stopped or restarted backend");
                    activation.Set();
                    Wait(delegate { return app.LogWindow.Visible; }, "second-launch activation");
                    Check(app.LogWindow.LogText.Contains("stderr: ready") && ReadLiveLog(logPath).Contains("stderr: ready"), "reopening the current tray session cleared its logs");
                    int child = Int32.Parse(File.ReadAllText(Path.Combine(args[0], "child-pid.txt")));
                    app.StopBackend(false, false);
                    Wait(delegate { return app.BackendProcessId == 0; }, "graceful stop");
                    Check(File.Exists(Path.Combine(args[0], "graceful-stop.txt")), "graceful shutdown signal was not received");
                    Wait(delegate { return Gone(child); }, "encoder child cleanup");
                    Check(!app.IsQuitting && app.LogWindow.Visible, "Stop must leave the tray/log window alive");
                    app.StartBackend();
                    Wait(delegate { return app.IsRunning && app.BackendProcessId != launcher; }, "start after stop");
                    launcher = app.BackendProcessId;
                    app.StopBackend(true, false);
                    Wait(delegate { return app.IsRunning && app.BackendProcessId != launcher; }, "restart");
                    Check(Directory.GetFiles(logDirectory, "sparkle-*.log").Length == 1, "backend restart or opening logs archived the active tray session");
                    app.StopBackend(false, true);
                    Wait(delegate { return app.BackendProcessId == 0; }, "quit");
                    Check(app.IsQuitting, "Quit did not end application lifetime");
                }
                string[] sessionArchives = Directory.GetFiles(logDirectory, "sparkle-*.log");
                Check(sessionArchives.Length == 2, "tray exit did not archive its session exactly once");
                foreach (string archive in sessionArchives)
                {
                    if (archive.EndsWith("-recovered.log", StringComparison.Ordinal)) continue;
                    string name = Path.GetFileNameWithoutExtension(archive).Substring("sparkle-".Length);
                    DateTime exitedAt = DateTime.ParseExact(name, LogBuffer.ArchiveTimeFormat, CultureInfo.InvariantCulture,
                        DateTimeStyles.AssumeUniversal | DateTimeStyles.AdjustToUniversal);
                    Check(exitedAt >= sessionStartedAt && exitedAt <= DateTime.UtcNow, "archive filename does not record the tray exit time");
                    string content = ReadLiveLog(archive);
                    Check(content.Contains("tray exited at " + exitedAt.ToString("O", CultureInfo.InvariantCulture)), "archive omitted its exit timestamp");
                    Check(Count(content, "graceful shutdown complete") == 3 && Count(content, "final stderr cleanup") == 3,
                        "archive lost final backend stdout/stderr across stop, restart, or quit");
                }
                using (var failed = new TrayApplication(args[0], Path.Combine(args[0], "missing.exe"), null))
                {
                    Check(!failed.IsRunning && failed.LogWindow.Visible, "startup errors must open the log window");
                    Check(failed.LogWindow.LogText.Contains("Unable to start"), "startup error details missing from logs");
                    Check(!failed.LogWindow.LogText.Contains("stderr: ready") && !ReadLiveLog(logPath).Contains("stderr: ready"), "a subsequent tray session kept earlier backend logs");
                    failed.LogWindow.Close();
                    Check(!failed.IsQuitting, "closing failed-start logs must preserve the tray");
                    failed.StopBackend(false, true);
                }
                using (var buffer = new LogBuffer(Path.Combine(args[0], "bounded-logs")))
                {
                    for (int i = 0; i < 3000; i++) buffer.Write("test", new String('x', 200));
                    long cursor = 0;
                    bool reset;
                    Check(buffer.Read(ref cursor, out reset).Length <= LogBuffer.MaxCharacters && reset, "log display memory must be bounded");
                    Check(buffer.Read(ref cursor, out reset) == "", "unchanged logs should not be re-rendered");
                }
                CheckRebuild(args[0], args[1]);
                CheckLogRetention(args[0]);
                File.WriteAllText(result, "PASS: hidden startup and children; fresh session logs; five exit-timestamped archives; recovery, retention, collision and locked-file safety; final stdout/stderr; UTF-8 live logs; close/reopen; activation; stop/start/restart/quit; graceful shutdown; process-tree cleanup; bounded display logs; rebuild success, failure, replacement failure, stopped recovery, duplicate suppression, hidden compiler children and quit cancellation.");
            }
            catch (Exception error) { File.WriteAllText(result, "FAIL: " + error); Environment.ExitCode = 1; }
        }
        private static ToolStripMenuItem MenuItem(TrayApplication app, string text)
        {
            foreach (ToolStripItem item in app.TrayMenu.Items)
                if (item.Text == text) return (ToolStripMenuItem)item;
            throw new Exception("Missing tray menu item: " + text);
        }
        private static int StartBuild(TrayApplication app, string root, EventWaitHandle gate, string mode)
        {
            gate.Reset();
            File.Delete(Path.Combine(root, "compiler-pid.txt"));
            File.Delete(Path.Combine(root, "compiler-child-console.txt"));
            File.WriteAllText(Path.Combine(root, "build-mode.txt"), mode);
            var rebuild = MenuItem(app, "Rebuild and Restart Sparkle Transcoder");
            Check(rebuild.Enabled, "rebuild should be available while running or stopped");
            rebuild.PerformClick();
            Wait(delegate { return File.Exists(Path.Combine(root, "compiler-pid.txt")) && File.Exists(Path.Combine(root, "compiler-child-console.txt")); }, "compiler startup");
            Check(app.IsRebuilding && !rebuild.Enabled, "rebuild must disable duplicate requests");
            Check(!MenuItem(app, "Start Sparkle Transcoder").Enabled && !MenuItem(app, "Stop Sparkle Transcoder").Enabled && !MenuItem(app, "Restart Sparkle Transcoder").Enabled,
                "lifecycle actions must stay disabled during rebuild");
            Check(MenuItem(app, "Quit").Enabled && MenuItem(app, "Open Logs").Enabled, "rebuild blocked logs or Quit");
            Check(File.ReadAllText(Path.Combine(root, "compiler-console.txt")) == "0" && File.ReadAllText(Path.Combine(root, "compiler-child-console.txt")) == "0",
                "build opened a console window");
            app.RebuildBackend();
            Check(Directory.GetDirectories(root, ".rebuild-*").Length == 1, "duplicate rebuild created another stage");
            return Int32.Parse(File.ReadAllText(Path.Combine(root, "compiler-child-pid.txt")));
        }
        private static void CheckRebuild(string root, string executable)
        {
            string gateName = "Local\\SparkleTest.Build." + Guid.NewGuid().ToString("N");
            Environment.SetEnvironmentVariable("SPARKLE_TEST_BUILD_EVENT", gateName);
            string template = Path.Combine(root, "NextBackend.exe");
            File.AppendAllText(template, "rebuilt fixture version");
            using (var gate = new EventWaitHandle(false, EventResetMode.ManualReset, gateName))
            using (var app = new TrayApplication(root, executable, null))
            {
                Wait(delegate { app.LogWindow.RefreshLogs(); return app.LogWindow.LogText.Contains("stderr: ready"); }, "rebuild backend startup");
                string original = Convert.ToBase64String(File.ReadAllBytes(executable));
                int launcher = app.BackendProcessId;
                int child = StartBuild(app, root, gate, "fail");
                Check(app.IsRunning && app.BackendProcessId == launcher, "build stopped the backend before compilation finished");
                app.StartBackend();
                app.StopBackend(true, false);
                Check(app.BackendProcessId == launcher, "lifecycle command interrupted rebuild");
                app.ShowLogs();
                app.LogWindow.Close();
                Check(app.IsRebuilding && !app.LogWindow.Visible, "logs were not responsive during rebuild");
                gate.Set();
                Wait(delegate { return !app.IsRebuilding; }, "failed build");
                Wait(delegate { return Gone(child); }, "failed compiler child cleanup");
                Check(app.IsRunning && app.BackendProcessId == launcher && Convert.ToBase64String(File.ReadAllBytes(executable)) == original,
                    "failed compilation changed or restarted the backend");
                Check(app.LogWindow.Visible && app.LogWindow.LogText.Contains("fixture compilation error"), "compiler errors did not open logs");

                // Prevent replacement while still permitting the existing executable to run.
                using (var locked = new FileStream(executable, FileMode.Open, FileAccess.Read, FileShare.Read))
                {
                    StartBuild(app, root, gate, "success");
                    gate.Set();
                    Wait(delegate { return !app.IsRebuilding && app.IsRunning && app.BackendProcessId != launcher; }, "replacement failure recovery");
                    Check(Convert.ToBase64String(File.ReadAllBytes(executable)) == original, "replacement failure damaged installed backend");
                    app.LogWindow.RefreshLogs();
                    Check(app.LogWindow.LogText.Contains("Could not install the build; restarting the previous backend"), "replacement failure was not reported");
                }
                launcher = app.BackendProcessId;
                child = StartBuild(app, root, gate, "success");
                Check(app.BackendProcessId == launcher && app.IsRunning, "successful build stopped backend too early");
                gate.Set();
                Wait(delegate { return !app.IsRebuilding && app.IsRunning && app.BackendProcessId != launcher; }, "successful rebuild restart");
                Wait(delegate { return Gone(child); }, "successful compiler child cleanup");
                Check(Convert.ToBase64String(File.ReadAllBytes(executable)) == Convert.ToBase64String(File.ReadAllBytes(template)), "rebuild did not install new binary");
                Check(!File.Exists(Path.Combine(root, "Sparkle.exe")), "backend rebuild unexpectedly rebuilt the tray");

                app.StopBackend(false, false);
                Wait(delegate { return app.BackendProcessId == 0; }, "stop before rebuild");
                File.Delete(executable);
                StartBuild(app, root, gate, "success");
                gate.Set();
                Wait(delegate { return !app.IsRebuilding && app.IsRunning; }, "rebuild missing backend while stopped");
                child = StartBuild(app, root, gate, "success");
                int compiler = Int32.Parse(File.ReadAllText(Path.Combine(root, "compiler-pid.txt")));
                MenuItem(app, "Quit").PerformClick();
                Wait(delegate { return app.IsQuitting && !app.IsRebuilding && app.BackendProcessId == 0; }, "quit during rebuild");
                Wait(delegate { return Gone(compiler) && Gone(child); }, "cancelled compiler tree cleanup");
                Check(Directory.GetDirectories(root, ".rebuild-*").Length == 0, "rebuild left staged binaries behind");
            }
            Environment.SetEnvironmentVariable("SPARKLE_TEST_BUILD_EVENT", null);
        }
        private static string ArchivePath(string directory, DateTime timestamp, string suffix)
        {
            return Path.Combine(directory, "sparkle-" + timestamp.ToString(LogBuffer.ArchiveTimeFormat, CultureInfo.InvariantCulture) + suffix + ".log");
        }
        private static void CheckLogRetention(string root)
        {
            string directory = Path.Combine(root, "retention");
            var exitedAt = new DateTime(2001, 2, 3, 4, 5, 6, DateTimeKind.Utc);
            using (var buffer = new LogBuffer(directory))
            {
                string unrelated = Path.Combine(directory, "sparkle-unrelated.log");
                string invalidDate = Path.Combine(directory, "sparkle-1999-99-99_00-00-00.0000000Z.log");
                File.WriteAllText(unrelated, "keep");
                File.WriteAllText(invalidDate, "keep");
                string nested = Path.Combine(directory, "nested");
                Directory.CreateDirectory(nested);
                File.WriteAllText(ArchivePath(nested, exitedAt.AddDays(-1), ""), "keep");
                for (int i = 0; i < 8; i++)
                {
                    buffer.Write("test", "session " + i);
                    buffer.EndSession(exitedAt.AddSeconds(i));
                    // Retention must use the filename's exit time, not file dates.
                    File.SetLastWriteTimeUtc(ArchivePath(directory, exitedAt.AddSeconds(i), ""), exitedAt.AddDays(-i));
                }
                Check(Directory.GetFiles(directory, "sparkle-2001-*.log").Length == 5, "retention did not keep exactly five archives");
                for (int i = 0; i < 8; i++)
                {
                    string path = ArchivePath(directory, exitedAt.AddSeconds(i), "");
                    if (i < 3) Check(!File.Exists(path), "oldest archive was retained");
                    else Check(ReadLiveLog(path).Contains("session " + i), "newest archive was lost");
                }
                Check(File.Exists(unrelated) && File.Exists(invalidDate) && Directory.GetFiles(nested).Length == 1,
                    "retention deleted unrelated or nested files");
                Check(ReadLiveLog(Path.Combine(directory, "sparkle.log")) == "", "archived content remained in the current log");
            }
            Check(Directory.GetFiles(directory, "sparkle-2001-*.log").Length == 5, "disposing an archived session added a duplicate archive");

            directory = Path.Combine(root, "collisions");
            using (var buffer = new LogBuffer(directory))
            {
                for (int i = 0; i < 8; i++)
                {
                    buffer.Write("test", "collision " + i);
                    buffer.EndSession(exitedAt);
                }
                Check(Directory.GetFiles(directory, "sparkle-*.log").Length == 5, "timestamp collision bypassed retention");
                for (int i = 0; i < 8; i++)
                {
                    string path = ArchivePath(directory, exitedAt, i == 0 ? "" : "-" + i.ToString("D4", CultureInfo.InvariantCulture));
                    if (i < 3) Check(!File.Exists(path), "timestamp collision kept an older log");
                    else Check(ReadLiveLog(path).Contains("collision " + i), "timestamp collision overwrote or discarded a newer log");
                }
            }

            directory = Path.Combine(root, "locked-log");
            var locked = new LogBuffer(directory);
            string current = Path.Combine(directory, "sparkle.log");
            locked.Write("test", "preserve despite blocked rename");
            using (var viewer = new FileStream(current, FileMode.Open, FileAccess.Read, FileShare.ReadWrite))
            {
                locked.Dispose();
                Check(ReadLiveLog(current).Contains("preserve despite blocked rename"), "failed exit rotation discarded the current log");
                using (var next = new LogBuffer(directory))
                {
                    Check(ReadLiveLog(current).Contains("preserve despite blocked rename"), "failed startup rotation truncated the previous log");
                    Check(ReadLiveLog(current).Contains("Could not archive or prune logs"), "blocked rotation was not reported");
                }
            }
            using (var next = new LogBuffer(directory))
            {
                string[] recovered = Directory.GetFiles(directory, "sparkle-*-recovered.log");
                Check(recovered.Length == 1 && ReadLiveLog(recovered[0]).Contains("preserve despite blocked rename"),
                    "previous log was not recovered after the viewer closed");
                Check(!ReadLiveLog(current).Contains("preserve despite blocked rename"), "recovery did not start a fresh current log");
                // The memory limit must never truncate the full disk log.
                next.Write("test", new String('x', LogBuffer.MaxCharacters * 2) + "disk log tail");
                Check(ReadLiveLog(current).Contains("disk log tail"), "display bounding truncated the disk log");
            }
        }
        private static int Count(string text, string term) { return (text.Length - text.Replace(term, "").Length) / term.Length; }
        private static string ReadLiveLog(string path)
        {
            using (var input = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite))
            using (var reader = new StreamReader(input)) return reader.ReadToEnd();
        }
        private static void Check(bool condition, string message) { if (!condition) throw new Exception(message); }
        private static bool Gone(int pid) { try { using (var process = Process.GetProcessById(pid)) return process.HasExited; } catch (ArgumentException) { return true; } }
        private static void Wait(Func<bool> condition, string label)
        {
            var timeout = Stopwatch.StartNew();
            while (timeout.ElapsedMilliseconds < 20000)
            {
                Application.DoEvents();
                if (condition()) return;
                Thread.Sleep(20);
            }
            throw new Exception("Timed out: " + label);
        }
    }
}
