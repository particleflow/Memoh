<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import uiTheme from './harmony-theme.json'
import { anchors, fragment, hitShapes, followVoiceTarget, releaseClip, releaseProgress, voiceFanGeometry, voiceFanMaterial, voiceSplashImpact, wrapPaths, type VoiceOrigin, type VoiceChoice, type VoiceFinger, type VoiceTarget, type EditorBounds } from './harmony-voice-geometry'
import './voiceFan.css'
const props = defineProps<{origin: VoiceOrigin; choice: VoiceChoice; finger: VoiceFinger; elapsed: number; peaks: number[]; closing: boolean; textTarget?: VoiceTarget; sentTarget?: VoiceTarget}>()
const id = 'harmony-fan-' + crypto.randomUUID().replaceAll('-', '')
const overlay = ref<HTMLDivElement>()
const transferOverlay = ref<HTMLDivElement>()
let entered = false
function values() {
  const { origin, choice, finger, elapsed, peaks, closing, textTarget, sentTarget } = props
  const seconds = Math.floor(elapsed / 1000)
  const waveform = peaks.slice(-11)
  const level = Math.max(0, ...waveform.slice(-3)) / 1024
  const scale = origin.radius / voiceFanGeometry.outerRadius
  const lift = origin.lift ?? 0
  const direction = origin.hand === 'right' ? -1 : 1
  const material = voiceFanMaterial(origin, textTarget)
  const transferTarget = choice === 'text' ? textTarget : choice === 'send' ? sentTarget : undefined
  const transferAnchor = choice === 'send' ? anchors.send : anchors.text
  const impact = transferTarget ? voiceSplashImpact(origin, choice === 'send' ? 'send' : 'text', transferTarget) : undefined
  return { origin, choice, finger, elapsed, peaks, closing, textTarget, sentTarget, seconds, waveform, level, scale, lift, direction, material, transferTarget, transferAnchor, impact }
}
const view = computed(values)
const px = (value: number) => value + 'px'
const position = (name: keyof typeof anchors) => { const v = view.value; return { left: px(v.origin.x + v.direction * anchors[name].x * v.scale), top: px(v.origin.y + (anchors[name].y - v.lift) * v.scale) } }
const paint = computed(() => { const { material, lift, scale } = view.value; return fragment
  .replace(hitShapes.outline, material.outline)
  .replace(/data-hit="(send|text)"/g, `$& transform="translate(0,${-lift})"`)
  .replace(/y="-244" width="488" height="488"/g, `y="${-244-lift}" width="488" height="${488+lift}"`)
  .replace(/cy="0" r="244"/, `cy="${-lift}" r="244"`)
  .replace('<g clip-path=', `<g transform="translate(0,${-lift})" clip-path=`)
  .replace(/(data-contact="editor" d=")[^"]+/, `$1${material.contact}`)
  .replace(/data-contact="microphone" cx="0" cy="0" r="22"/, `data-contact="microphone" cx="0" cy="0" r="${voiceFanGeometry.microphoneRadius/scale}"`)
  .replace('x1="18" y1="0" x2="244" y2="0"', `x1="${material.editor.x}" y1="0" x2="${material.editor.x+material.editor.width}" y2="0"`)
  .replace('translate(.5 .5) scale(.00204918)', `translate(${244/material.width} ${(244+lift)/(488+lift)}) scale(${1/material.width} ${1/(488+lift)})`)
  .replace(/__id__/g,id)
})
const fanTheme = document.documentElement.classList.contains('dark') ? uiTheme.defaultDark : uiTheme.defaultLight
const fan = uiTheme.themes[fanTheme as keyof typeof uiTheme.themes].fan
const styleVars = {
  '--ui-text': 'var(--foreground)', '--ui-accent': 'var(--primary)', '--ui-fan-surface': 'var(--background)',
  '--ui-scrim': 'color-mix(in srgb, var(--background) 35%, transparent)',
  '--ui-layer-recording-backdrop': String(uiTheme.shared.layers.recordingBackdrop), '--ui-layer-recording-feedback': String(uiTheme.shared.layers.recordingFeedback),
  '--ui-feedback-duration': uiTheme.shared.motion.feedbackMs + 'ms', '--ui-ease': uiTheme.shared.motion.easing,
  '--ui-enter-duration': uiTheme.shared.motion.enterMs+'ms', '--ui-transparent': uiTheme.shared.transparent,
  '--ui-voice-breath-duration': uiTheme.shared.voiceMotion.breathMs+'ms', '--ui-voice-ripple-duration': uiTheme.shared.voiceMotion.rippleMs+'ms',
  '--ui-voice-meter-duration': uiTheme.shared.voiceMotion.meterResponseMs+'ms', '--ui-voice-backdrop-blur': uiTheme.shared.voiceMotion.backdropBlurPx+'px',
  '--ui-voice-release-feather': uiTheme.shared.voiceMotion.releaseFeatherPx+'px', '--ui-fan-blur': fan.blurFar+'px', '--ui-fan-opacity': String(fan.opacityNear), '--ui-fan-feather': (fan.featherStart*100)+'%'
}
let transferFinished: Promise<void> | undefined
function feedbackFinished(): Promise<void> {
  return transferFinished ?? Promise.reject(new DOMException('转字反馈未开始', 'AbortError'))
}
defineExpose({ feedbackFinished })
function animateTransfer() {
  transferFinished = undefined
  const { origin, choice, closing, scale, lift, direction, transferTarget, transferAnchor } = values()

    const root = transferOverlay.value
    if (!root || !closing || !transferTarget) return undefined
    const motion = uiTheme.shared.voiceMotion;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const ball = root.querySelector<HTMLElement>('.voice-injection-ball');
    const splash = root.querySelector<SVGSVGElement>('.voice-injection-splash');
    const wrap = root.querySelector<SVGSVGElement>('.voice-injection-wrap');
    const edges = Array.from(root.querySelectorAll<SVGPathElement>('.voice-injection-wrap path'));
    if (!ball || !splash || !wrap || !edges.length) return undefined;
    const sending = choice === 'send';
    const absorbMs = sending ? motion.sendSplash.absorbMs : motion.injectAbsorbMs;
    // The fan remains in the composer's existing stacking context. Measure its
    // displayed anchor before the flight enters the viewport-level layer.
    const source = overlay.value?.querySelector(`[data-action="${choice}"]`)?.closest('.voice-anchor')?.getBoundingClientRect()
    const fromX = source ? source.x + source.width / 2 : origin.x + direction * transferAnchor.x * scale
    const fromY = source ? source.y + source.height / 2 : origin.y + (transferAnchor.y - lift) * scale
    ball.style.left = `${fromX}px`
    ball.style.top = `${fromY}px`
    const flightOrigin = { ...origin, x: fromX - direction * transferAnchor.x * scale,
      y: fromY - (transferAnchor.y - lift) * scale }
    const impact = voiceSplashImpact(flightOrigin, sending ? 'send' : 'text', transferTarget)
    const ballFrames = (target: EditorBounds) => {
      const dx = target.x + target.width / 2 - fromX;
      const dy = target.y - fromY;
      const { rise } = voiceSplashImpact(flightOrigin, sending ? 'send' : 'text', target)
      return [
        { transform: 'translate(0px, 0px) scale(0)', opacity: 0, easing: motion.entryEasing },
        {
          transform: 'translate(0px, 0px) scale(1)',
          opacity: 1,
          offset: 0.18,
          easing: motion.injectRiseEasing,
        },
        {
          transform: `translate(0px, ${-rise}px) scale(.85, 1.15)`,
          opacity: 1,
          offset: 0.4,
          easing: motion.injectFallEasing,
        },
        { transform: `translate(${dx}px, ${dy}px) scale(1.3, .65)`, opacity: 1, offset: 0.92 },
        { transform: `translate(${dx}px, ${dy}px) scale(0)`, opacity: 0 },
      ];
    };
    const animations = [
      ball?.animate(ballFrames(transferTarget), {
        duration: reduced ? 100 : motion.injectFallMs,
        easing: 'linear',
        fill: 'both',
      }),
      splash?.animate(
        [
          { transform: 'scale(.2, .05)', opacity: 0 },
          {
            transform: sending
              ? `translateX(${impact.drift * 0.25}px) scale(.8, 1.15)`
              : 'scale(.8, 1.15)',
            opacity: 0.7,
            offset: 0.25,
          },
          {
            transform: sending
              ? `translateX(${impact.drift * 0.7}px) scale(1.2, .35)`
              : 'scale(1.2, .35)',
            opacity: 0.4,
            offset: 0.7,
          },
          {
            transform: sending ? `translateX(${impact.drift}px) scale(1.4, 0)` : 'scale(1.4, 0)',
            opacity: 0,
          },
        ],
        {
          duration: reduced ? 100 : absorbMs,
          delay: reduced ? 0 : motion.injectFallMs - 45,
          easing: motion.entryEasing,
          fill: 'both',
        }
      ),
      ...edges.map((edge, i) =>
        edge.animate(
          [
            { strokeDashoffset: 1, opacity: 0 },
            {
              strokeDashoffset: 0.7,
              opacity:
                sending && i !== (impact.side > 0 ? 0 : 1)
                  ? motion.sendSplash.trailingOpacity
                  : 0.6,
              offset: 0.25,
            },
            { strokeDashoffset: 0, opacity: 0.3, offset: 0.8 },
            { strokeDashoffset: 0, opacity: 0 },
          ],
          {
            duration: reduced
              ? 100
              : absorbMs *
                (sending && i === (impact.side > 0 ? 0 : 1) ? motion.sendSplash.leadingRatio : 1),
            delay: reduced
              ? 0
              : motion.injectFallMs +
                (sending && i !== (impact.side > 0 ? 0 : 1)
                  ? motion.sendSplash.trailingDelayMs
                  : 0),
            easing: motion.entryEasing,
            fill: 'both',
          }
        )
      ),
    ];
    const stopFollowing = followVoiceTarget(
      transferTarget,
      (target) => {
        // setKeyframes retains the animation clock; layout movement must not replay the effect.
        (animations[0]?.effect as KeyframeEffect | null)?.setKeyframes(ballFrames(target))
        if (splash) {
          splash.style.left = `${target.x + target.width / 2}px`
          splash.style.top = `${target.y}px`
        }
        if (wrap) {
          wrap.style.left = `${target.x - 3}px`
          wrap.style.top = `${target.y - 3}px`
          wrap.style.width = `${target.width + 6}px`
          wrap.style.height = `${target.height + 6}px`
          wrapPaths(target).forEach((path, index) => edges[index]?.setAttribute('d', path))
        }
      },
      () => {
        animations.forEach((animation) => animation?.cancel())
        ;[ball, splash, wrap].forEach((element) => {
          const style = element?.style
          if (style) style.visibility = 'hidden'
        })
      }
    )
    transferFinished = Promise.all(animations
      .filter((animation): animation is Animation => !!animation)
      .map(animation => animation.finished)).then(() => {});
    // Cleanup can cancel before the parent reads this promise. Keep rejection
    // observable by its consumer without an unhandled-rejection side effect.
    void transferFinished.catch(() => {});
    return () => {
      stopFollowing?.();
      animations.forEach((animation) => animation?.cancel());
    };
}
function animateSurface() {
  const { closing, lift, material } = values()

    const root = overlay.value;
    if (!root) return undefined;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const motion = uiTheme.shared.voiceMotion;
    const firstEntrance = !entered;
    entered = true;
    const elements = Array.from(root.querySelectorAll<HTMLElement>('[data-motion]'));
    const animations = elements.map((element) => {
      const part = element.dataset.motion;
      const { action } = element.dataset;
      const selected = closing && action === props.choice;
      const current = getComputedStyle(element);
      const matrix =
        current.transform === 'none'
          ? new window.DOMMatrixReadOnly()
          : new window.DOMMatrixReadOnly(current.transform);
      // Preserve all visible transform components when an in-flight motion is reversed.
      const scaleX = Math.hypot(matrix.a, matrix.b);
      const scaleY = scaleX === 0 ? 0 : (matrix.a * matrix.d - matrix.b * matrix.c) / scaleX;
      const rotation = (Math.atan2(matrix.b, matrix.a) * 180) / Math.PI;
      const start = {
        transform: `translate(${matrix.e}px, ${matrix.f}px) rotate(${rotation}deg) scale(${scaleX}, ${scaleY})`,
        opacity: current.opacity,
        clipPath: current.clipPath,
      };
      element.getAnimations().forEach((animation) => animation.cancel());
      let frames: {
        transform: string;
        opacity: string | number;
        clipPath?: string;
        offset?: number;
      }[];
      let duration = motion.enterMs;
      let delay = 0;
      let easing = closing ? motion.exitEasing : motion.entryEasing;
      if (closing) {
        duration = motion.materialExitMs;
        if (part === 'material') {
          frames = [
            { transform: 'none', clipPath: start.clipPath, opacity: start.opacity },
            { transform: 'none', clipPath: releaseClip(0, material.width, lift), opacity: 0 },
          ];
          if (props.choice !== 'cancel') delay = motion.confirmLeadMs;
        } else if (selected) {
          duration = motion.injectGatherMs;
          frames = [
            start,
            {
              transform: `translate(0px, ${part === 'icon' ? 18 : -18}px) rotate(${
                part === 'icon' ? 35 : -12
              }deg) scale(.6, .18)`,
              opacity: 0.8,
              offset: 0.6,
            },
            {
              transform: `translate(0px, ${
                part === 'icon' ? 22 : -22
              }px) rotate(70deg) scale(0, 0)`,
              opacity: 0,
            },
          ];
        } else {
          frames = [
            start,
            { transform: 'translate(0px, 6px) rotate(0deg) scale(0, 0)', opacity: 0 },
          ];
          if (part === 'scrim') {
            duration = motion.exitMs;
            frames = [
              { transform: 'none', opacity: start.opacity },
              { transform: 'none', opacity: 0 },
            ];
          }
        }
      } else if (part === 'material') {
        const from = firstEntrance ? 0 : releaseProgress(start.clipPath, lift);
        const opacity = firstEntrance ? 1 : Number(start.opacity);
        frames = Array.from({ length: 25 }, (_, i) => ({
          transform: 'none',
          clipPath: releaseClip(from + (1 - from) * (i / 24), material.width, lift),
          opacity: opacity + (1 - opacity) * (i / 24),
          offset: i / 24,
        }));
        if (!firstEntrance && frames[0]) frames[0].clipPath = start.clipPath;
      } else if (part === 'icon' || part === 'label') {
        duration = motion.actionsEnterMs;
        easing = motion.actionEasing;
        if (firstEntrance)
          delay = motion.actionsDelayMs + (part === 'label' ? motion.labelDelayMs : 0);
        const from = firstEntrance
          ? {
              transform: `translate(0px, 6px) rotate(${part === 'icon' ? -4 : 0}deg)`,
              clipPath: 'ellipse(0% 0% at 50% 55%)',
              opacity: 0,
            }
          : start;
        frames = [
          from,
          {
            transform: 'translate(0px, 0px) rotate(0deg)',
            clipPath: 'ellipse(85% 85% at 50% 55%)',
            opacity: 1,
          },
        ];
      } else {
        frames = [
          { transform: 'none', opacity: firstEntrance ? 0 : start.opacity },
          { transform: 'none', opacity: 1 },
        ];
      }
      if (reduced) {
        duration = 100;
        delay = 0;
        frames = [
          { transform: 'none', clipPath: 'none', opacity: firstEntrance ? 0 : start.opacity },
          { transform: 'none', clipPath: 'none', opacity: closing ? 0 : 1 },
        ];
      }
      return element.animate(frames, {
        duration,
        delay,
        easing,
        fill: 'both',
      });
    });
    return () => {
      if (!root.isConnected) animations.forEach((animation) => animation.cancel());
    };
}
let stopTransfer: (()=>void) | undefined
let stopSurface: (()=>void) | undefined
onMounted(() => { stopSurface = animateSurface(); stopTransfer = animateTransfer() })
watch([()=>props.closing, ()=>props.origin, ()=>props.textTarget, ()=>props.sentTarget], () => {
  stopTransfer?.(); stopTransfer = animateTransfer(); stopSurface?.(); stopSurface = animateSurface()
}, {flush:'post'})
onBeforeUnmount(() => { stopTransfer?.(); stopSurface?.(); overlay.value?.querySelectorAll('*').forEach(node => node.getAnimations().forEach(animation => animation.cancel())) })
</script>
<template>
  <div ref="overlay" class="voice-overlay harmony-voice-overlay" :style="styleVars" :data-choice="choice" :data-closing="closing" :data-hand="origin.hand ?? 'left'">
    <div class="voice-scrim" data-motion="scrim" />
    <div class="voice-fan">
      <div class="voice-release-feather" :style="{width:px(view.material.width*view.scale),height:px((488+view.lift)*view.scale),top:px(origin.y-origin.radius-view.lift*view.scale),left:px(origin.x-origin.radius+(view.direction===1?0:origin.radius*2-view.material.width*view.scale)),right:'auto',transform:view.direction===-1?'scaleX(-1)':undefined}">
        <div class="voice-material" data-motion="material" :style="{clipPath:releaseClip(0,view.material.width,view.lift)}">
          <div v-for="ring in [1,2,3]" :key="ring" :class="`voice-fan-blur voice-fan-blur-${ring}`" :style="{clipPath:`url(#${id}-blur-clip)`}" />
          <svg class="voice-fan-surface" :viewBox="`-244 ${-244-view.lift} ${view.material.width} ${488+view.lift}`" aria-hidden="true">
            <!-- Only the accepted local SVG artwork is inserted; never user/model HTML. -->
            <g v-html="paint" />
            <g :clip-path="`url(#${id}-clip)`">
              <g v-for="action in (['send','text'] as const)" :key="action" :class="`voice-feedback voice-feedback-${action}`" :clip-path="`url(#${id}-clip-${action})`" :mask="`url(#${id}-mask-${action})`">
                <use :href="`#${id}-region-${action}`" fill="var(--ui-accent)" opacity=".055" />
                <g class="voice-finger" :style="{transform:`translate(${view.direction*(finger.x-origin.x)/view.scale}px,${(finger.y-origin.y)/view.scale}px)`}"><use :href="`#${id}-feedback`" /></g>
              </g>
            </g>
          </svg>
        </div>
      </div>
      <div v-for="action in (['send','text'] as const)" :key="action" class="voice-anchor" :style="position(action)">
        <div class="voice-choice" :data-selected="choice===action"><div class="voice-choice-feedback">
          <svg data-motion="icon" :data-action="action" viewBox="0 0 24 24" width="28" height="28" aria-hidden="true"><use :href="`#${id}-${action}`" :transform="view.direction===-1&&action==='send'?'translate(24 0) scale(-1 1)':undefined" /></svg>
          <span data-motion="label" :data-action="action">{{action==='send'?'松开发送':'松开转字'}}</span>
        </div></div>
      </div>
      <div class="voice-anchor" :style="position('indicator')"><div class="voice-indicator" data-motion="indicator" :data-audible="view.level>.018">
        <svg viewBox="0 0 44 30" aria-label="录音波峰" :style="{transform:view.direction===-1?'scaleX(-1)':undefined}"><rect v-for="(_,i) in 11" :key="i" :x="i*4" y="2" width="2" height="26" rx="1" :style="{transform:`scaleY(${Math.max(3/26,(view.waveform[i]??0)<18?0:(view.waveform[i]??0)/1024)})`}" /></svg>
        <span>{{String(Math.floor(view.seconds/60)).padStart(2,'0')}}:{{String(view.seconds%60).padStart(2,'0')}}</span>
      </div></div>
    </div>
    <!-- DOM measurements and pointer events are viewport CSS pixels. Keep the
         flight outside the composer's first-send transform containing block. -->
    <Teleport to="body">
      <div ref="transferOverlay" class="voice-overlay" :style="styleVars">
      <template v-if="closing && view.transferTarget">
        <div class="voice-injection-ball" :style="{left:px(origin.x+view.direction*view.transferAnchor.x*view.scale),top:px(origin.y+(view.transferAnchor.y-view.lift)*view.scale)}" />
        <svg class="voice-injection-splash" :style="{left:px(view.transferTarget.x+view.transferTarget.width/2),top:px(view.transferTarget.y)}" viewBox="-48 -40 96 48" aria-hidden="true">
          <g :transform="choice==='send'?`scale(${view.impact?.side??view.direction},1)`:undefined"><path :d="choice==='send'?view.impact?.path:'M-40 2 Q-25 0 -21 -12 Q-14 -25 -13 -8 Q-9 0 0 0 Q9 0 13 -8 Q14 -25 21 -12 Q25 0 40 2'" /><circle cx="-24" cy="-26" r="2" /><circle :cx="20+(choice==='send'?(view.impact?.lean??0)*.7:0)" cy="-32" r="2.5" /></g>
        </svg>
        <svg class="voice-injection-wrap" :style="{left:px(view.transferTarget.x-3),top:px(view.transferTarget.y-3),width:px(view.transferTarget.width+6),height:px(view.transferTarget.height+6)}" aria-hidden="true"><path pathLength="1" :d="wrapPaths(view.transferTarget)[0]" /><path pathLength="1" :d="wrapPaths(view.transferTarget)[1]" /></svg>
      </template>
      </div>
    </Teleport>
  </div>
</template>
