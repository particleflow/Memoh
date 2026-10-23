import artwork from './voiceFan.svg?raw'
import uiTheme from './harmony-theme.json'

export type VoiceChoice = 'send' | 'text' | 'cancel';
export type VoiceHand = 'left' | 'right';
export type VoiceOrigin = { x: number; y: number; radius: number; hand?: VoiceHand; lift?: number };
export type VoiceFinger = { x: number; y: number };
export type EditorBounds = { x: number; y: number; width: number; height: number; radius?: number };
export type VoiceTarget = EditorBounds & { resolve?: () => EditorBounds | undefined };

// The message may move while its native echo is reconciled. Follow that same identity,
// without restarting the approved animation or waiting for transfer completion.
export function followVoiceTarget(
  target: VoiceTarget,
  update: (bounds: EditorBounds) => void,
  cancel: () => void
) {
  let frame: number | undefined;
  let previous: EditorBounds = target;
  let stopped = false;
  const stop = () => {
    stopped = true;
    if (frame !== undefined) cancelAnimationFrame(frame);
  };
  const inspect = () => {
    if (stopped) return;
    const next = target.resolve?.();
    if (!next) {
      stop();
      cancel();
      return;
    }
    if (
      next.x !== previous.x ||
      next.y !== previous.y ||
      next.width !== previous.width ||
      next.height !== previous.height ||
      next.radius !== previous.radius
    ) {
      update(next);
      previous = next;
    }
    frame = requestAnimationFrame(inspect);
  };
  if (target.resolve) frame = requestAnimationFrame(inspect);
  return stop;
}

export function wrapPaths(target: EditorBounds) {
  const { width, height } = target;
  const radius = Math.min(target.radius ?? 24, width / 2, height / 2);
  return [
    `M${width / 2 + 3} 3 H${width + 3 - radius} Q${width + 3} 3 ${width + 3} ${radius + 3}
      V${height + 3 - radius} Q${width + 3} ${height + 3} ${width + 3 - radius} ${height + 3} H${
      width / 2 + 3
    }`,
    `M${width / 2 + 3} 3 H${radius + 3} Q3 3 3 ${radius + 3}
      V${height + 3 - radius} Q3 ${height + 3} ${radius + 3} ${height + 3} H${width / 2 + 3}`,
  ];
}

// Layout is read from the editable artwork, never recreated in a separate preview.
function anchor(name: string) {
  const match = artwork.match(
    new RegExp(`data-anchor="${name}" data-x="([-\\d.]+)" data-y="([-\\d.]+)"`)
  );
  if (!match) throw new Error('VOICE_DESIGN_ANCHOR_MISSING');
  return { x: Number(match[1]), y: Number(match[2]) };
}
const geometryMatch = artwork.match(/<metadata id="voice-geometry">([^<]+)<\/metadata>/);
if (!geometryMatch?.[1]) throw new Error('VOICE_DESIGN_GEOMETRY_MISSING');
export const voiceFanGeometry = JSON.parse(geometryMatch[1]) as {
  center: [number, number];
  outerRadius: number;
  innerRadius: number;
  microphoneRadius: number;
  leftAngle: number;
  splitAngle: number;
  rightAngle: number;
  baselineY: number;
  innerRightAngle: number;
  editorInset: number;
  editorRadius: number;
  editorLeft: number;
  editorWidth: number;
  editorHeight: number;
};
export const anchors = {
  send: anchor('send'),
  text: anchor('text'),
  indicator: anchor('indicator'),
};
export function voiceFanLayout(
  point: Pick<VoiceOrigin, 'x' | 'y' | 'hand'>,
  width: number,
  visibleTop: number,
  target?: EditorBounds
): VoiceOrigin {
  const g = voiceFanGeometry;
  const widthRadius = Math.min(
    g.outerRadius,
    (point.hand === 'right' ? point.x : width - point.x) - uiTheme.shared.voiceInput.edgeInsetPx
  );
  const top = target?.y ?? point.y + (g.baselineY * widthRadius) / g.outerRadius;
  // Fit the complete fixed artwork above the actual field, including the system safe area.
  // Text/icons keep their size; material, hit regions and feedback share this one scale.
  const indicatorClearance = g.baselineY - anchors.indicator.y;
  const heightRadius =
    (top - visibleTop - g.editorInset - indicatorClearance) /
    (1 + (g.baselineY - indicatorClearance) / g.outerRadius);
  const radius = Math.max(g.microphoneRadius, Math.min(widthRadius, heightRadius));
  const scale = radius / g.outerRadius;
  const lift =
    Math.max(0, (point.y - top) / scale + g.baselineY) +
    (g.editorInset + indicatorClearance * (1 - scale)) / scale;
  return { ...point, radius, lift };
}
// A curved release front uncovers fixed artwork along the editor edge.
// The material and its microphone join never scale or rotate.
export function releaseClip(
  progress: number,
  materialWidth = voiceFanGeometry.outerRadius * 2,
  lift = 0
): string {
  const { outerRadius: radius, microphoneRadius, baselineY } = voiceFanGeometry;
  const width = microphoneRadius + (materialWidth - microphoneRadius) * progress;
  const height = (radius * 1.35 + lift) * progress;
  const xs = Array.from({ length: 49 }, (_, i) => -radius + (radius * 2 * i) / 48);
  const points = xs.map((x) => {
    const edge = Math.max(0, Math.min(1, (Math.abs(x) - width) / 36));
    const y =
      baselineY +
      1 -
      height * Math.max(0, 1 - (x / width) ** 2) +
      (radius - baselineY) * edge * edge * (3 - 2 * edge);
    return `${((x + radius) / materialWidth) * 100}% ${
      ((y + radius + lift) / (radius * 2 + lift)) * 100
    }%`;
  });
  return `polygon(${points.join(',')},100% 100%,0% 100%)`;
}
export function releaseProgress(clip: string, lift = 0): number {
  const coordinates = clip.match(/[-\d.]+(?=%)/g);
  if (!coordinates || coordinates.length < 102) return 0;
  const { outerRadius: radius, baselineY } = voiceFanGeometry;
  const centerY = (Number(coordinates[49]) / 100) * (radius * 2 + lift) - radius - lift;
  return Math.max(0, Math.min(1, (baselineY + 1 - centerY) / (radius * 1.35 + lift)));
}
function hitPath(name: string): string {
  const match = artwork.match(new RegExp(`data-hit="${name}" d="([^"]+)"`));
  if (!match?.[1]) throw new Error('VOICE_DESIGN_REGION_MISSING');
  return match[1];
}
export const hitShapes = { outline: hitPath('outline'), send: hitPath('send'), text: hitPath('text') };
// The material follows the editor only where the natural fan reaches it.
// Its microphone edge is one continuous curve; the far editor stays untouched.
export function voiceFanMaterial(origin: VoiceOrigin, target?: EditorBounds) {
  const g = voiceFanGeometry;
  const scale = origin.radius / g.outerRadius;
  const microphoneScale = 1 / scale;
  const direction = origin.hand === 'right' ? -1 : 1;
  const lift = origin.lift ?? 0;
  const left = target
    ? Math.min(
        direction * (target.x - origin.x),
        direction * (target.x + target.width - origin.x)
      ) / scale
    : g.editorLeft;
  const right = target ? left + target.width / scale : left + g.editorWidth;
  const top = target ? (target.y - origin.y) / scale : g.baselineY;
  const bottom = target ? (target.y + target.height - origin.y) / scale : top + g.editorHeight;
  const x0 = left - g.editorInset;
  const x1 = Math.min(right + g.editorInset, g.outerRadius);
  const y0 = top - g.editorInset;
  const y1 = bottom + g.editorInset;
  const editorRadius = Math.min(
    target
      ? (target.radius ?? g.editorRadius - g.editorInset) / scale
      : g.editorRadius - g.editorInset,
    (bottom - top) / 2
  );
  const r = Math.min(editorRadius + g.editorInset, (y1 - y0) / 2, (x1 - x0) / 2);
  const roundedRect = (x: number, y: number, width: number, height: number, radius: number) =>
    `M ${x + radius} ${y} H ${x + width - radius} A ${radius} ${radius} 0 0 1 ${x + width} ${
      y + radius
    }
    V ${y + height - radius} A ${radius} ${radius} 0 0 1 ${x + width - radius} ${y + height}
    H ${x + radius} A ${radius} ${radius} 0 0 1 ${x} ${y + height - radius}
    V ${y + radius} A ${radius} ${radius} 0 0 1 ${x + radius} ${y} Z`;
  const contactOuter = roundedRect(x0, y0, x1 - x0, y1 - y0, r);
  const outline = `M ${-21.665771 * microphoneScale} ${-3.82026 * microphoneScale}
    C ${-18.5 * microphoneScale} ${-21.8 * microphoneScale} ${-20 * microphoneScale} ${
    -142 - lift
  } -33.958237 ${-241.625409 - lift}
    A 244 244 0 0 1 242.181261 ${-29.73612 - lift} C 243 ${-22 - lift} ${x1} -12 ${x1} -2
    V ${y1 - r} Q ${x1} ${y1} ${x1 - r} ${y1} H ${x0 + r}
    C ${x0 + 8} ${y1} ${18.445 * microphoneScale} ${19.087 * microphoneScale} ${
    3.82026 * microphoneScale
  } ${21.665771 * microphoneScale} A ${22 * microphoneScale} ${22 * microphoneScale} 0 0 1 ${
    -21.665771 * microphoneScale
  } ${-3.82026 * microphoneScale} Z
    ${contactOuter}`;
  return {
    outline,
    contact: contactOuter,
    width: g.outerRadius * 2,
    editor: { x: x0, y: y0, width: x1 - x0, height: y1 - y0 },
  };
}
// The last flight segment defines incidence; reflection keeps its horizontal momentum.
export function voiceSplashImpact(
  origin: VoiceOrigin,
  choice: 'send' | 'text',
  target: EditorBounds
) {
  const scale = origin.radius / voiceFanGeometry.outerRadius;
  const hand = origin.hand === 'right' ? -1 : 1;
  const fromX = origin.x + hand * anchors[choice].x * scale;
  const fromY = origin.y + (anchors[choice].y - (origin.lift ?? 0)) * scale;
  const dx = target.x + target.width / 2 - fromX;
  const dy = target.y - fromY;
  const motion = uiTheme.shared.voiceMotion;
  // Preserve the accepted short rise for a normal field; clear a taller destination
  // before descending into its actual top edge rather than flying upward through it.
  const rise = Math.max(motion.injectRisePx, motion.landingClearancePx - dy);
  const speed = Math.hypot(dx, dy + rise);
  const bias = Math.abs(dx) / speed;
  const side = Math.sign(dx) || hand;
  const detail = uiTheme.shared.voiceMotion.sendSplash;
  // Keep the accepted small splash silhouette; incident direction only nudges its forward lobe.
  const stretch = 1 + detail.lobeBias * bias;
  const lean = detail.leanPx * bias;
  return {
    dx,
    dy,
    rise,
    side,
    drift: side * detail.driftPx * bias,
    lean,
    path: `M-40 2 Q-25 0 -21 -12 Q-14 -25 -13 -8 Q-9 0 0 0
      Q9 0 ${13 * stretch + lean * 0.4} -8 Q${14 * stretch + lean} ${-25 * stretch}
      ${21 * stretch + lean * 0.5} -12 Q${25 * stretch} 0 40 2`,
  };
}
let hitContext: CanvasRenderingContext2D | null;
let hitPaths: Record<keyof typeof hitShapes, Path2D> | undefined;
export function voiceFanChoice(origin: VoiceOrigin, x: number, y: number): VoiceChoice {
  if (!hitPaths) {
    hitContext = document.createElement('canvas').getContext('2d');
    hitPaths = {
      outline: new window.Path2D(hitShapes.outline),
      send: new window.Path2D(hitShapes.send),
      text: new window.Path2D(hitShapes.text),
    };
  }
  const scale = origin.radius / voiceFanGeometry.outerRadius;
  const px = ((x - origin.x) / scale) * (origin.hand === 'right' ? -1 : 1);
  const py = (y - origin.y) / scale + (origin.lift ?? 0);
  if (!hitContext?.isPointInPath(hitPaths.outline, px, py)) return 'cancel';
  if (hitContext.isPointInPath(hitPaths.send, px, py)) return 'send';
  if (hitContext.isPointInPath(hitPaths.text, px, py)) return 'text';
  return 'cancel';
}
export const fragment = artwork.slice(artwork.indexOf('>') + 1, artwork.lastIndexOf('</svg>'));
