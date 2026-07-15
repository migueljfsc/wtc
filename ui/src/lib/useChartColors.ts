import { useTheme } from "@/components/ThemeProvider";

/**
 * Chart colors, resolved per theme. SVG presentation attributes don't resolve
 * CSS variables, so Recharts gets literal values. The two series colors are
 * validated (dataviz): CVD-separable and inside the lightness band in both
 * modes. "failed" is a reserved status hue — always paired with a legend label.
 */
export function useChartColors() {
  const { theme } = useTheme();
  const isDark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);

  return {
    events: isDark ? "#3b82f6" : "#2563eb", // primary series
    failed: "#ef4444", // status: failed
    grid: isDark ? "#27272a" : "#eceef1", // recessive gridlines
    axis: isDark ? "#8b8b93" : "#9aa1ab", // recessive axis text
    surface: isDark ? "#020817" : "#ffffff", // card bg — the gap between stacked fills
  };
}
