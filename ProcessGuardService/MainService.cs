using ProcessGuard.Common;
using ProcessGuard.Common.Models;
using ProcessGuard.Common.Utility;
using System;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Linq;
using System.Runtime.InteropServices;
using System.Security.AccessControl;
using System.Security.Principal;
using System.ServiceProcess;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace ProcessGuardService
{
    public partial class MainService : ServiceBase
    {
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool CloseHandle(IntPtr hObject);

        private Task _guardianTask = null;
        private NamedPipeServerStream _namedPipeServer = null;
        private readonly ConcurrentDictionary<string, ConfigItemWithProcessId> _startedProcesses = new ConcurrentDictionary<string, ConfigItemWithProcessId>();
        private readonly ConcurrentDictionary<string, ConfigItemWithProcessId> _changedProcesses = new ConcurrentDictionary<string, ConfigItemWithProcessId>();

        public MainService()
        {
            InitializeComponent();
        }

        private bool _running;

        protected override void OnStart(string[] args)
        {
            _running = true;
            _guardianTask = Task.Factory.StartNew(StartGuardian, TaskCreationOptions.LongRunning);
            _ = Task.Factory.StartNew(StartNamedPipeServer, TaskCreationOptions.LongRunning);
            _ = Task.Factory.StartNew(StartLogPipeServer, TaskCreationOptions.LongRunning);
        }

        private void StartGuardian()
        {
            LoadConfig();

            while (_running)
            {
                Thread.Sleep(3000);

                // Check cron schedules
                CheckCronSchedules();

                foreach (var keyValuePair in _startedProcesses)
                {
                    var config = keyValuePair.Value;

                    if (config.ProcessId > 0)
                    {
                        try
                        {
                            using (var process = Process.GetProcessById(config.ProcessId))
                            {
                                // Just for dispose
                            }
                        }
                        catch (Exception)
                        {
                            // The process has not started, should be restarted
                            config.ProcessId = 0;
                        }
                    }

                    if (config.ProcessId <= 0)
                    {
                        // If this item has a cron but StopBeforeCronExec is false,
                        // it's a run-once-per-trigger task. Don't auto-restart.
                        if (!string.IsNullOrEmpty(config.CronExpression) && !config.StopBeforeCronExec)
                        {
                            continue;
                        }

                        StartProcess(config, keyValuePair.Key);
                    }
                }

                var changeInfos = _changedProcesses.ToList();

                foreach (var changeInfo in changeInfos)
                {
                    if (changeInfo.Value.ChangeType.HasFlag(ChangeType.Stop))
                    {
                        try
                        {
                            _startedProcesses.TryGetValue(changeInfo.Key, out var startedProcess);
                            if (startedProcess != null && startedProcess.ProcessId > 0)
                            {
                                using (var process = Process.GetProcessById(startedProcess.ProcessId))
                                {
                                    process.Kill();
                                }
                            }
                        }
                        catch (Exception)
                        {
                            // do nothing
                        }
                    }

                    if (changeInfo.Value.ChangeType.HasFlag(ChangeType.Start))
                    {
                        // Preserve output buffer from existing process if any
                        ConfigItemWithProcessId existing;
                        if (_startedProcesses.TryGetValue(changeInfo.Key, out existing) && existing.OutputBuffer != null)
                        {
                            changeInfo.Value.OutputBuffer = existing.OutputBuffer;
                        }
                        _startedProcesses[changeInfo.Key] = changeInfo.Value;
                    }

                    if (changeInfo.Value.ChangeType.HasFlag(ChangeType.Remove))
                    {
                        ConfigItemWithProcessId removed;
                        if (_startedProcesses.TryRemove(changeInfo.Key, out removed))
                        {
                            CleanupProcessPipe(removed);
                        }
                    }

                    _changedProcesses.TryRemove(changeInfo.Key, out _);
                }
            }
        }

        /// <summary>
        /// Start a process and set up output capture for NoWindow processes
        /// </summary>
        private void StartProcess(ConfigItemWithProcessId config, string key)
        {
            var startFilePath = config.EXEFullPath;

            if (!File.Exists(startFilePath))
                return;

            IntPtr outputReadPipe;
            ApplicationLoader.StartProcessInSession0(startFilePath, Path.GetDirectoryName(startFilePath), out var processInfo, config.Minimize,
                string.IsNullOrEmpty(config.StartupParams) ? null : $" {config.StartupParams}", config.NoWindow, out outputReadPipe);

            if (processInfo.dwProcessId > 0)
            {
                config.ProcessId = (int)processInfo.dwProcessId;

                // Set up output capture for NoWindow processes
                if (config.NoWindow && outputReadPipe != IntPtr.Zero)
                {
                    // Clean up old pipe if exists
                    CleanupProcessPipe(config);

                    config.OutputReadPipe = outputReadPipe;
                    if (config.OutputBuffer == null)
                    {
                        config.OutputBuffer = new CircularLineBuffer(5000);
                    }

                    // Start background thread to read output
                    var pipeHandle = outputReadPipe;
                    var buffer = config.OutputBuffer;
                    var configId = config.Id;
                    Task.Factory.StartNew(() => ReadPipeOutput(pipeHandle, buffer, configId), TaskCreationOptions.LongRunning);
                }

                // Parse cron expression if present
                if (!string.IsNullOrEmpty(config.CronExpression) && config.CronInstance == null)
                {
                    CronParser parser;
                    if (CronParser.TryParse(config.CronExpression, out parser))
                    {
                        config.CronInstance = parser;
                    }
                }

                if (config.OnlyOpenOnce)
                {
                    _changedProcesses.TryAdd(key, new ConfigItemWithProcessId
                    {
                        Id = config.Id,
                        ChangeType = ChangeType.Remove,
                    });
                }
            }
        }

        /// <summary>
        /// Read output from a pipe handle into a circular buffer and log file
        /// </summary>
        private void ReadPipeOutput(IntPtr pipeHandle, CircularLineBuffer buffer, string configId)
        {
            StreamWriter fileWriter = null;
            try
            {
                var logFilePath = ConfigHelper.GetLogFilePath(configId);
                fileWriter = new StreamWriter(logFilePath, true, new UTF8Encoding(false));
                fileWriter.AutoFlush = true;
                int lineCount = 0;

                using (var stream = new FileStream(new Microsoft.Win32.SafeHandles.SafeFileHandle(pipeHandle, true), FileAccess.Read))
                using (var reader = new StreamReader(stream))
                {
                    string line;
                    while ((line = reader.ReadLine()) != null)
                    {
                        buffer.AddLine(line);
                        try { fileWriter.WriteLine(line); } catch { }
                        lineCount++;

                        // Rotate log file when it exceeds 512KB
                        if (lineCount % 500 == 0)
                        {
                            try
                            {
                                fileWriter.Flush();
                                if (new FileInfo(logFilePath).Length > 512 * 1024)
                                {
                                    fileWriter.Close();
                                    fileWriter = null;
                                    File.Delete(logFilePath);
                                    fileWriter = new StreamWriter(logFilePath, false, new UTF8Encoding(false));
                                    fileWriter.AutoFlush = true;
                                    fileWriter.Write(buffer.GetLastLines(1000));
                                    fileWriter.Flush();
                                }
                            }
                            catch { }
                        }
                    }
                }
            }
            catch (Exception)
            {
                // Pipe closed or process exited
            }
            finally
            {
                try { fileWriter?.Dispose(); } catch { }
            }
        }

        /// <summary>
        /// Clean up pipe handle for a process
        /// </summary>
        private void CleanupProcessPipe(ConfigItemWithProcessId config)
        {
            if (config.OutputReadPipe != IntPtr.Zero)
            {
                try
                {
                    CloseHandle(config.OutputReadPipe);
                }
                catch { }
                config.OutputReadPipe = IntPtr.Zero;
            }
        }

        /// <summary>
        /// Check cron schedules and trigger executions
        /// </summary>
        private void CheckCronSchedules()
        {
            var now = DateTime.Now;

            foreach (var keyValuePair in _startedProcesses)
            {
                var config = keyValuePair.Value;

                if (string.IsNullOrEmpty(config.CronExpression))
                    continue;

                // Parse cron if not yet parsed
                if (config.CronInstance == null)
                {
                    CronParser parser;
                    if (!CronParser.TryParse(config.CronExpression, out parser))
                        continue;
                    config.CronInstance = parser;
                }

                // Check if cron matches current time
                if (!config.CronInstance.Matches(now))
                    continue;

                // Avoid duplicate triggers within the same minute
                if (config.LastCronExecution.Year == now.Year &&
                    config.LastCronExecution.Month == now.Month &&
                    config.LastCronExecution.Day == now.Day &&
                    config.LastCronExecution.Hour == now.Hour &&
                    config.LastCronExecution.Minute == now.Minute)
                    continue;

                config.LastCronExecution = now;

                // Stop existing process if configured
                if (config.StopBeforeCronExec && config.ProcessId > 0)
                {
                    try
                    {
                        using (var process = Process.GetProcessById(config.ProcessId))
                        {
                            process.Kill();
                        }
                    }
                    catch (Exception) { }
                    config.ProcessId = 0;
                }

                // For non-stop tasks, only start if not running
                if (!config.StopBeforeCronExec && config.ProcessId > 0)
                {
                    bool isRunning = false;
                    try
                    {
                        using (var process = Process.GetProcessById(config.ProcessId))
                        {
                            isRunning = true;
                        }
                    }
                    catch { }

                    if (isRunning)
                        continue;
                }

                config.ProcessId = 0;
                StartProcess(config, keyValuePair.Key);
            }
        }

        /// <summary>
        /// Load the config file content to the dictionary
        /// </summary>
        private void LoadConfig()
        {
            var configList = ConfigHelper.LoadConfigFile();

            foreach (var item in configList)
            {
                if (item.Started)
                {
                    var configWithPid = new ConfigItemWithProcessId
                    {
                        EXEFullPath = item.EXEFullPath,
                        Id = item.Id,
                        Minimize = item.Minimize,
                        NoWindow = item.NoWindow,
                        OnlyOpenOnce = item.OnlyOpenOnce,
                        ProcessName = item.ProcessName,
                        Started = item.Started,
                        StartupParams = item.StartupParams,
                        CronExpression = item.CronExpression,
                        StopBeforeCronExec = item.StopBeforeCronExec,
                    };

                    // Parse cron expression
                    if (!string.IsNullOrEmpty(item.CronExpression))
                    {
                        CronParser parser;
                        if (CronParser.TryParse(item.CronExpression, out parser))
                        {
                            configWithPid.CronInstance = parser;
                        }
                    }

                    _startedProcesses[item.Id] = configWithPid;
                }
            }
        }

        /// <summary>
        /// Start the NamedPipeServer, listen to the changes of the config
        /// </summary>
        private void StartNamedPipeServer()
        {
            _namedPipeServer = new NamedPipeServerStream(Constants.PROCESS_GUARD_SERVICE, PipeDirection.In);
            _namedPipeServer.WaitForConnection();
            StreamReader reader = new StreamReader(_namedPipeServer);

            while (_running)
            {
                try
                {
                    var line = reader.ReadLine();
                    if (line == null)
                    {
                        throw new IOException("The client disconnected");
                    }

                    var config = line.DeserializeObject<ConfigItemWithProcessId>();
                    if (config.Started)
                    {
                        config.ChangeType = ChangeType.Start;

                        if (_startedProcesses.ContainsKey(config.Id))
                        {
                            config.ChangeType |= ChangeType.Stop;
                        }
                    }
                    else
                    {
                        config.ChangeType = ChangeType.Stop | ChangeType.Remove;
                    }

                    _changedProcesses[config.Id] = config;
                }
                catch (IOException)
                {
                    _namedPipeServer.Dispose();
                    reader.Dispose();
                    _namedPipeServer = new NamedPipeServerStream(Constants.PROCESS_GUARD_SERVICE, PipeDirection.In);
                    _namedPipeServer.WaitForConnection();
                    reader = new StreamReader(_namedPipeServer);
                }
            }
        }

        /// <summary>
        /// Named pipe server for log retrieval requests from the GUI
        /// </summary>
        private void StartLogPipeServer()
        {
            var noBom = new UTF8Encoding(false);

            while (_running)
            {
                NamedPipeServerStream logPipe = null;
                try
                {
                    try
                    {
                        var pipeSecurity = new PipeSecurity();
                        pipeSecurity.AddAccessRule(new PipeAccessRule(
                            new SecurityIdentifier(WellKnownSidType.WorldSid, null),
                            PipeAccessRights.FullControl,
                            AccessControlType.Allow));

                        logPipe = new NamedPipeServerStream(Constants.PROCESS_GUARD_LOG_PIPE,
                            PipeDirection.InOut, NamedPipeServerStream.MaxAllowedServerInstances,
                            PipeTransmissionMode.Byte, PipeOptions.None, 4096, 4096, pipeSecurity);
                    }
                    catch
                    {
                        // Fallback: create without explicit security (matches config pipe pattern)
                        logPipe = new NamedPipeServerStream(Constants.PROCESS_GUARD_LOG_PIPE,
                            PipeDirection.InOut, NamedPipeServerStream.MaxAllowedServerInstances);
                    }

                    logPipe.WaitForConnection();

                    var reader = new StreamReader(logPipe, noBom, false, 1024, true);
                    var configId = reader.ReadLine();

                    string logContent = string.Empty;
                    if (!string.IsNullOrEmpty(configId))
                    {
                        ConfigItemWithProcessId process;
                        if (_startedProcesses.TryGetValue(configId, out process) && process.OutputBuffer != null)
                        {
                            logContent = process.OutputBuffer.GetLastLines(1000);
                        }
                    }

                    var writer = new StreamWriter(logPipe, noBom, 1024, true);
                    writer.Write(logContent);
                    writer.Flush();
                }
                catch (Exception)
                {
                    // Connection error, continue listening
                }
                finally
                {
                    try { logPipe?.Dispose(); } catch { }
                }
            }
        }

        protected override void OnStop()
        {
            _running = false;
            _namedPipeServer.Dispose();
            while (!_guardianTask.IsCompleted)
            {
                Thread.Sleep(1);
            }
        }
    }
}
