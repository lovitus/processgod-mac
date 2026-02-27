using System;
using ProcessGuard.Common.Utility;
using Newtonsoft.Json;

namespace ProcessGuard.Common.Models
{
    public class ConfigItemWithProcessId : ConfigItem
    {
        /// <summary>
        /// The processId of started process
        /// </summary>
        public int ProcessId { get; set; }

        /// <summary>
        /// The config change type
        /// </summary>
        public ChangeType ChangeType { get; set; }

        /// <summary>
        /// In-memory circular buffer for captured output (NoWindow processes)
        /// </summary>
        [JsonIgnore]
        public CircularLineBuffer OutputBuffer { get; set; }

        /// <summary>
        /// The read end of the output pipe (NoWindow processes)
        /// </summary>
        [JsonIgnore]
        public IntPtr OutputReadPipe { get; set; }

        /// <summary>
        /// Last cron execution time (to avoid duplicate triggers within same minute)
        /// </summary>
        [JsonIgnore]
        public DateTime LastCronExecution { get; set; }

        /// <summary>
        /// Parsed cron instance (cached)
        /// </summary>
        [JsonIgnore]
        public CronParser CronInstance { get; set; }
    }

    /// <summary>
    /// The config change type
    /// </summary>
    [Flags]
    public enum ChangeType
    {
        None = 0,

        /// <summary>
        /// The process should be started
        /// </summary>
        Start = 1,

        /// <summary>
        /// The process should be stopped
        /// </summary>
        Stop = 2,

        /// <summary>
        /// The process should be removed from the guard list
        /// </summary>
        Remove = 4,
    }
}
