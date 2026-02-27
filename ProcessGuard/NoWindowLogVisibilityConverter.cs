using System;
using System.Globalization;
using System.Windows;
using System.Windows.Data;

namespace ProcessGuard
{
    /// <summary>
    /// Converter that returns Visible when both NoWindow and Started are true
    /// </summary>
    public class NoWindowLogVisibilityConverter : IMultiValueConverter
    {
        public object Convert(object[] values, Type targetType, object parameter, CultureInfo culture)
        {
            if (values.Length == 2 && values[0] is bool noWindow && values[1] is bool started)
            {
                return (noWindow && started) ? Visibility.Visible : Visibility.Collapsed;
            }
            return Visibility.Collapsed;
        }

        public object[] ConvertBack(object value, Type[] targetTypes, object parameter, CultureInfo culture)
        {
            throw new NotImplementedException();
        }
    }
}
