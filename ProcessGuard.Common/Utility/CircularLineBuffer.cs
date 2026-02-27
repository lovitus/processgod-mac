using System;
using System.Collections.Generic;
using System.Text;

namespace ProcessGuard.Common.Utility
{
    /// <summary>
    /// Thread-safe circular buffer that stores the last N lines of text
    /// </summary>
    public class CircularLineBuffer
    {
        private readonly string[] _buffer;
        private readonly int _capacity;
        private int _head;
        private int _count;
        private readonly object _lock = new object();

        public CircularLineBuffer(int capacity = 5000)
        {
            _capacity = capacity;
            _buffer = new string[capacity];
            _head = 0;
            _count = 0;
        }

        /// <summary>
        /// Add a line to the buffer
        /// </summary>
        public void AddLine(string line)
        {
            lock (_lock)
            {
                _buffer[_head] = line;
                _head = (_head + 1) % _capacity;
                if (_count < _capacity)
                    _count++;
            }
        }

        /// <summary>
        /// Get the last N lines from the buffer
        /// </summary>
        public string GetLastLines(int lineCount = 1000)
        {
            lock (_lock)
            {
                if (_count == 0)
                    return string.Empty;

                int actualCount = Math.Min(lineCount, _count);
                var sb = new StringBuilder();

                // Calculate the start index
                int startIndex = (_head - actualCount + _capacity) % _capacity;

                for (int i = 0; i < actualCount; i++)
                {
                    int index = (startIndex + i) % _capacity;
                    if (i > 0)
                        sb.AppendLine();
                    sb.Append(_buffer[index] ?? string.Empty);
                }

                return sb.ToString();
            }
        }

        /// <summary>
        /// Clear all lines
        /// </summary>
        public void Clear()
        {
            lock (_lock)
            {
                _head = 0;
                _count = 0;
                Array.Clear(_buffer, 0, _capacity);
            }
        }
    }
}
