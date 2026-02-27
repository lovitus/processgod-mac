namespace ProcessGuard.Common
{
    public class Constants
    {
        /// <summary>
        /// 配置文件名称
        /// </summary>
        public const string CONFIG_FILE_NAME = "GuardianConfig.json";

        /// <summary>
        /// The global configurations file name
        /// </summary>
        public const string GLOBAL_CONFIG_FILE_NAME = "GlobalConfig.json";

        /// <summary>
        /// 服务进程名
        /// </summary>
        public const string PROCESS_GUARD_SERVICE = "ProcessGuardService";

        /// <summary>
        /// 服务文件名
        /// </summary>
        public const string FILE_GUARD_SERVICE = "ProcessGuardService.exe";

        /// <summary>
        /// Named pipe for log retrieval
        /// </summary>
        public const string PROCESS_GUARD_LOG_PIPE = "ProcessGuardService_Logs";

        /// <summary>
        /// Fallback named pipe for log retrieval
        /// </summary>
        public const string PROCESS_GUARD_LOG_PIPE_FALLBACK = "ProcessGuardService_Logs_Fallback";

        /// <summary>
        /// Shared memory map name prefix for log snapshots
        /// </summary>
        public const string PROCESS_GUARD_LOG_MMF_PREFIX = "Global\\ProcessGuardService_Logs_";

        /// <summary>
        /// Fallback shared memory map prefix for environments where Global namespace is unavailable
        /// </summary>
        public const string PROCESS_GUARD_LOG_MMF_PREFIX_FALLBACK = "Local\\ProcessGuardService_Logs_";

        /// <summary>
        /// Shared memory size for log snapshots (bytes)
        /// </summary>
        public const int PROCESS_GUARD_LOG_MMF_SIZE = 512 * 1024;
    }
}
