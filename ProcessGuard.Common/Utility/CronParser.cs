using System;
using System.Collections.Generic;
using System.Linq;

namespace ProcessGuard.Common.Utility
{
    /// <summary>
    /// Simple cron expression parser supporting 5-field format:
    /// minute hour day-of-month month day-of-week
    /// Supports: * (any), */n (step), n (specific), n,m (list), n-m (range)
    /// </summary>
    public class CronParser
    {
        private readonly HashSet<int> _minutes;
        private readonly HashSet<int> _hours;
        private readonly HashSet<int> _daysOfMonth;
        private readonly HashSet<int> _months;
        private readonly HashSet<int> _daysOfWeek;

        public CronParser(string expression)
        {
            if (string.IsNullOrWhiteSpace(expression))
                throw new ArgumentException("Cron expression cannot be empty.");

            var parts = expression.Trim().Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries);
            if (parts.Length != 5)
                throw new ArgumentException("Cron expression must have exactly 5 fields: minute hour day-of-month month day-of-week");

            _minutes = ParseField(parts[0], 0, 59);
            _hours = ParseField(parts[1], 0, 23);
            _daysOfMonth = ParseField(parts[2], 1, 31);
            _months = ParseField(parts[3], 1, 12);
            _daysOfWeek = ParseField(parts[4], 0, 7);

            // Normalize: treat 7 as 0 (both mean Sunday)
            if (_daysOfWeek.Contains(7))
            {
                _daysOfWeek.Add(0);
                _daysOfWeek.Remove(7);
            }
        }

        /// <summary>
        /// Check if the given time matches this cron expression
        /// </summary>
        public bool Matches(DateTime time)
        {
            int dow = (int)time.DayOfWeek; // Sunday=0
            return _minutes.Contains(time.Minute)
                && _hours.Contains(time.Hour)
                && _daysOfMonth.Contains(time.Day)
                && _months.Contains(time.Month)
                && _daysOfWeek.Contains(dow);
        }

        /// <summary>
        /// Validate a cron expression string without throwing
        /// </summary>
        public static bool TryParse(string expression, out CronParser parser)
        {
            parser = null;
            try
            {
                parser = new CronParser(expression);
                return true;
            }
            catch
            {
                return false;
            }
        }

        private static HashSet<int> ParseField(string field, int min, int max)
        {
            var result = new HashSet<int>();

            foreach (var part in field.Split(','))
            {
                var trimmed = part.Trim();

                if (trimmed == "*")
                {
                    for (int i = min; i <= max; i++)
                        result.Add(i);
                }
                else if (trimmed.StartsWith("*/"))
                {
                    int step;
                    if (!int.TryParse(trimmed.Substring(2), out step) || step <= 0)
                        throw new ArgumentException($"Invalid step value in: {field}");
                    for (int i = min; i <= max; i += step)
                        result.Add(i);
                }
                else if (trimmed.Contains("-"))
                {
                    var rangeParts = trimmed.Split('-');
                    if (rangeParts.Length != 2)
                        throw new ArgumentException($"Invalid range in: {field}");
                    int start, end;
                    if (!int.TryParse(rangeParts[0], out start) || !int.TryParse(rangeParts[1], out end))
                        throw new ArgumentException($"Invalid range values in: {field}");
                    if (start < min || end > max || start > end)
                        throw new ArgumentException($"Range out of bounds in: {field}");
                    for (int i = start; i <= end; i++)
                        result.Add(i);
                }
                else
                {
                    int value;
                    if (!int.TryParse(trimmed, out value))
                        throw new ArgumentException($"Invalid value in: {field}");
                    if (value < min || value > max)
                        throw new ArgumentException($"Value {value} out of range [{min}-{max}] in: {field}");
                    result.Add(value);
                }
            }

            return result;
        }
    }
}
