// Deterministic monogram colour derived from the title — shared by the
// launcher list and the settings plugin list.
export function monogramStyle(title: string) {
  let h = 0;
  for (const ch of title) h = (h * 31 + ch.charCodeAt(0)) % 360;
  return {
    background: `linear-gradient(135deg, hsl(${h} 52% 46%), hsl(${(h + 40) % 360} 52% 32%))`,
  };
}
