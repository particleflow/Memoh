import { common } from '@kit.AbilityKit';
import { buffer } from '@kit.ArkTS';
export interface NativeThemeColors { background: string; content: string; }
interface ThemeEntry { native: NativeThemeColors; }
interface ThemeDocument { version: number; defaultLight: string; defaultDark: string; themes: Record<string, ThemeEntry>; }
export class UiThemes {
  private document: ThemeDocument;
  constructor(context: common.UIAbilityContext) {
    const bytes = context.resourceManager.getRawFileContentSync('ui-theme.json');
    this.document = JSON.parse(buffer.from(bytes).toString()) as ThemeDocument;
    if (this.document.version !== 1) { throw new Error('NATIVE_THEME_INVALID'); }
  }
  get(id: string): NativeThemeColors {
    if (!Object.keys(this.document.themes).includes(id)) { throw new Error('NATIVE_THEME_INVALID'); }
    const theme = this.document.themes[id];
    if (!theme || !/^#[a-fA-F0-9]{6}$/.test(theme.native.background) ||
      !/^#[a-fA-F0-9]{6}$/.test(theme.native.content)) { throw new Error('NATIVE_THEME_INVALID'); }
    return theme.native;
  }
}
