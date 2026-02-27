using ProcessGuard.Common;
using ProcessGuard.Common.Models;
using ProcessGuard.Common.Utility;
using System;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.IO;
using System.IO.MemoryMappedFiles;
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
            _ = Task.Factory.StartNew(() => StartLogPipeServer(Constants.PROCESS_GUARD_LOG_PIPE, true), TaskCreationOptions.LongRunning);
            _ = Task.Factory.StartNew(() => StartLogPipeServer(Constants.PROCESS_GUARD_LOG_PIPE_FALLBACK, false), TaskCreationOptions.LongRunning);
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
                        config.OutputBuffer = new CircularLineBuffer(1000);
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
        /// Read output from a pipe handle into a circular buffer (memory only)
        /// </summary>
        private void ReadPipeOutput(IntPtr pipeHandle, CircularLineBuffer buffer, string configId)
        {
            try
            {
                var batchCount = 0;
                var totalCount = 0;
                using (var stream = new FileStream(new Microsoft.Win32.SafeHandles.SafeFileHandle(pipeHandle, true), FileAccess.Read))
                using (var reader = new StreamReader(stream))
                {
                    string line;
                    while ((line = reader.ReadLine()) != null)
                    {
                        buffer.AddLine(line);
                        batchCount++;
                        totalCount++;

                        if (totalCount <= 5 || batchCount >= 20)
                        {
                            WriteSharedMemorySnapshot(configId, buffer.GetLastLines(1000));
                            batchCount = 0;
                        }
                    }
                }

                WriteSharedMemorySnapshot(configId, buffer.GetLastLines(1000));
            }
            catch (Exception)
            {
                // Pipe closed or process exited
            }
        }

        private static string[] GetLogMapNames(string configId)
        {
            return new[]
            {
                Constants.PROCESS_GUARD_LOG_MMF_PREFIX + configId,
                Constants.PROCESS_GUARD_LOG_MMF_PREFIX_FALLBACK + configId,
            };
        }

        private void WriteSharedMemorySnapshot(string configId, string logContent)
        {
            if (string.IsNullOrEmpty(configId))
                return;

            try
            {
                var bytes = Encoding.UTF8.GetBytes(logContent ?? string.Empty);
                var maxPayload = Constants.PROCESS_GUARD_LOG_MMF_SIZE - sizeof(int);
                if (bytes.Length > maxPayload)
                {
                    var truncated = new byte[maxPayload];
                    Buffer.BlockCopy(bytes, bytes.Length - maxPayload, truncated, 0, maxPayload);
                    bytes = truncated;
                }

                foreach (var mapName in GetLogMapNames(configId))
                {
                    try
                    {
                        MemoryMappedFile mmf;

                        try
                        {
                            var security = new MemoryMappedFileSecurity();
                            security.AddAccessRule(new AccessRule<MemoryMappedFileRights>(
                                new SecurityIdentifier(WellKnownSidType.BuiltinAdministratorsSid, null),
                                MemoryMappedFileRights.FullControl,
                                AccessControlType.Allow));
                            security.AddAccessRule(new AccessRule<MemoryMappedFileRights>(
                                new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null),
                                MemoryMappedFileRights.FullControl,
                                AccessControlType.Allow));
                            security.AddAccessRule(new AccessRule<MemoryMappedFileRights>(
                                new SecurityIdentifier(WellKnownSidType.AuthenticatedUserSid, null),
                                MemoryMappedFileRights.ReadWrite,
                                AccessControlType.Allow));
                            security.AddAccessRule(new AccessRule<MemoryMappedFileRights>(
                                new SecurityIdentifier(WellKnownSidType.BuiltinUsersSid, null),
                                MemoryMappedFileRights.ReadWrite,
                                AccessControlType.Allow));

                            mmf = MemoryMappedFile.CreateOrOpen(
                                mapName,
                                Constants.PROCESS_GUARD_LOG_MMF_SIZE,
                                MemoryMappedFileAccess.ReadWrite,
                                MemoryMappedFileOptions.None,
                                security,
                                HandleInheritability.None);
                        }
                        catch
                        {
                            mmf = MemoryMappedFile.CreateOrOpen(mapName, Constants.PROCESS_GUARD_LOG_MMF_SIZE);
                        }

                        using (mmf)
                        using (var view = mmf.CreateViewStream(0, Constants.PROCESS_GUARD_LOG_MMF_SIZE, MemoryMappedFileAccess.Write))
                        using (var writer = new BinaryWriter(view, Encoding.UTF8, true))
                        {
                            writer.Write(bytes.Length);
                            writer.Write(bytes);
                            writer.Flush();
                        }
                    }
                    catch
                    {
                        // try next map name
                    }
                }
            }
            catch
            {
                // Shared memory is best-effort fallback
            }
        }

        private string ReadSharedMemorySnapshot(string configId)
        {
            if (string.IsNullOrEmpty(configId))
                return string.Empty;

            try
            {
                foreach (var mapName in GetLogMapNames(configId))
                {
                    try
                    {
                        using (var mmf = MemoryMappedFile.OpenExisting(mapName, MemoryMappedFileRights.Read))
                        using (var view = mmf.CreateViewStream(0, 0, MemoryMappedFileAccess.Read))
                        using (var reader = new BinaryReader(view, Encoding.UTF8, true))
                        {
                            if (view.Length < sizeof(int))
                                continue;

                            var length = reader.ReadInt32();
                            var maxPayload = Constants.PROCESS_GUARD_LOG_MMF_SIZE - sizeof(int);
                            if (length <= 0 || length > maxPayload)
                                continue;

                            var bytes = reader.ReadBytes(length);
                            return Encoding.UTF8.GetString(bytes);
                        }
                    }
                    catch
                    {
                        // try next map name
                    }
                }
            }
            catch
            {
                // ignore
            }

            return string.Empty;
        }

        private string GetLogSnapshot(string configId)
        {
            if (string.IsNullOrEmpty(configId))
                return string.Empty;

            ConfigItemWithProcessId process;
            if (_startedProcesses.TryGetValue(configId, out process) && process.OutputBuffer != null)
            {
                return process.OutputBuffer.GetLastLines(1000);
            }

            return ReadSharedMemorySnapshot(configId);
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
        /// Named pipe server for log retrieval requests from the GUI.
        /// Two instances are started with different pipe names for fallback.
        /// </summary>
        private void StartLogPipeServer(string pipeName, bool useWorldSecurity)
        {
            var noBom = new UTF8Encoding(false);

            try
            {
                while (_running)
                {
                    NamedPipeServerStream logPipe = null;
                    try
                    {
                        logPipe = CreateLogPipeServer(pipeName, useWorldSecurity);
                        logPipe.WaitForConnection();

                        var reader = new StreamReader(logPipe, noBom, false, 1024, true);
                        var configId = reader.ReadLine();
                        var logContent = GetLogSnapshot(configId);

                        var writer = new StreamWriter(logPipe, noBom, 1024, true);
                        writer.Write(logContent);
                        writer.Flush();
                    }
                    catch
                    {
                        // continue listening on failures
                    }
                    finally
                    {
                        try { logPipe?.Dispose(); } catch { }
                    }
                }
            }
            catch
            {
                // do nothing
            }
        }

        private NamedPipeServerStream CreateLogPipeServer(string pipeName, bool useWorldSecurity)
        {
            if (useWorldSecurity)
            {
                try
                {
                    var pipeSecurity = new PipeSecurity();
                    pipeSecurity.AddAccessRule(new PipeAccessRule(
                        new SecurityIdentifier(WellKnownSidType.BuiltinAdministratorsSid, null),
                        PipeAccessRights.FullControl, AccessControlType.Allow));
                    pipeSecurity.AddAccessRule(new PipeAccessRule(
                        new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null),
                        PipeAccessRights.FullControl, AccessControlType.Allow));
                    pipeSecurity.AddAccessRule(new PipeAccessRule(
                        new SecurityIdentifier(WellKnownSidType.AuthenticatedUserSid, null),
                        PipeAccessRights.ReadWrite, AccessControlType.Allow));
                    pipeSecurity.AddAccessRule(new PipeAccessRule(
                        new SecurityIdentifier(WellKnownSidType.BuiltinUsersSid, null),
                        PipeAccessRights.ReadWrite, AccessControlType.Allow));

                    return new NamedPipeServerStream(
                        pipeName,
                        PipeDirection.InOut,
                        NamedPipeServerStream.MaxAllowedServerInstances,
                        PipeTransmissionMode.Byte,
                        PipeOptions.None,
                        4096,
                        4096,
                        pipeSecurity);
                }
                catch
                {
                    // If complex ACL fails, still use InOut so client can read/write,
                    // but it might inherit default SYSTEM ACL.
                    return new NamedPipeServerStream(
                        pipeName,
                        PipeDirection.InOut,
                        NamedPipeServerStream.MaxAllowedServerInstances);
                }
            }

            return new NamedPipeServerStream(
                pipeName,
                PipeDirection.InOut,
                NamedPipeServerStream.MaxAllowedServerInstances);
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
