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
        /// TCP port for log retrieval (localhost only)
        /// </summary>
        public const int LOG_TCP_PORT = 39213;
    }
}
