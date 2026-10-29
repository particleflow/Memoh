import { NativeClient } from '../common/NativeClient';
import { ImageTimingDump } from '../common/ImageTimingDump';
import { NativePhotoSelection } from '../common/NativePhotoSelection';
import { common } from '@kit.AbilityKit';
import { webview } from '@kit.ArkWeb';
import { ClientDiagnostics } from '../common/ClientDiagnostics';
import { WEB_RESOURCES } from '../common/WebResourceManifest';
import { WEB_START, packagedPath, memohRequest, viewerRequest, sharedWebAvailable, externalBrowserLink } from '../common/SharedWebPolicy';
import { fileIo as fs, picker } from '@kit.CoreFileKit';
import { window } from '@kit.ArkUI';
import { BusinessError } from '@kit.BasicServicesKit';
import { UiThemes } from '../common/UiTheme';
import { NativeWebNetwork } from '../common/NativeWebNetwork';


@Entry
@Component
struct SharedChat {
  private controller: webview.WebviewController = new webview.WebviewController();
  private context: common.UIAbilityContext = getContext(this) as common.UIAbilityContext;
  private photoSelection: NativePhotoSelection = new NativePhotoSelection(this.context);
  private probeGeneration: number = 0;
  private nativeClient?: NativeClient;
  private webNetwork: NativeWebNetwork = new NativeWebNetwork((): boolean =>
    this.active && this.nativeReady && !this.loadFailed && this.webSource === WEB_START);
  private downloadDelegate?: webview.WebDownloadDelegate;
  private webDownloads: Map<string, webview.WebDownloadItem> = new Map();
  @State status: string = '正在打开聊天…';
  @State webSource: string = WEB_START;
  @State probing: boolean = false;
  @State nativeReady: boolean = false;
  @State chatReady: boolean = false;
  @State loginReady: boolean = false;
  @State renderWaiting: boolean = false;
  private clientTimer: number = -1;
  private clientStage: string = 'document';
  private loadFailed: boolean = false;
  private active: boolean = true;
  private chatInsetTop: number = 0;
  private chatInsetBottom: number = 0;
  private navigationInsetBottom: number = 0;
  private keyboardWindow?: window.Window;
  private keyboardHeightChanged: (height: number) => void = (height: number) => {
    if (!this.active || !Number.isInteger(height) || height < 0) { return; }
    this.chatInsetBottom = height > 0 ? 0 : this.navigationInsetBottom;
    this.applyChatInsets();
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'keyboard-insets',
      'keyboardHeightPx=' + height + ';bottom=' + this.chatInsetBottom);
  };
  private windowPrepared: boolean = false;
  private windowPreparing?: Promise<void>;
  private themes: UiThemes = new UiThemes(this.context);
  @State surfaceColor: string = this.themes.get('light-theme').background;
  @State textColor: string = this.themes.get('light-theme').content;
  private themeTransition: Promise<void> = Promise.resolve();

  private setClientTheme(id: string): Promise<void> {
    const theme = this.themes.get(id);
    this.themeTransition = this.themeTransition.catch(() => {}).then(async () => {
      if (!this.active) { return; }
      await this.prepareWindow();
      const main = await window.getLastWindow(this.context);
      main.setWindowBackgroundColor(theme.background);
      await main.setWindowSystemBarProperties({
        statusBarColor: theme.background, statusBarContentColor: theme.content,
        navigationBarColor: theme.background, navigationBarContentColor: theme.content,
        enableStatusBarAnimation: false, enableNavigationBarAnimation: false
      });
      if (!this.active) { return; }
      this.surfaceColor = theme.background; this.textColor = theme.content;
      ClientDiagnostics.event('SYSTEM', 'appearance', 'theme-applied', 'id=' + id);
    });
    return this.themeTransition;
  }

  private async prepareWindow(): Promise<void> {
    if (this.windowPrepared) { return; }
    if (this.windowPreparing) { return this.windowPreparing; }
    this.windowPreparing = this.configureWindow();
    try { await this.windowPreparing; }
    finally { this.windowPreparing = undefined; }
  }

  private async configureWindow(): Promise<void> {
    const main = await window.getLastWindow(this.context);
    if (!this.active) { return; }
    const area = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM);
    const navigation = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR);
    this.chatInsetTop = px2vp(area.topRect.height);
    this.navigationInsetBottom = px2vp(Math.max(area.bottomRect.height, navigation.bottomRect.height));
    await main.setWindowLayoutFullScreen(false);
    if (!this.active) { return; }
    main.on('keyboardHeightChange', this.keyboardHeightChanged);
    this.keyboardWindow = main;
    try {
      const keyboard = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_KEYBOARD);
      this.chatInsetBottom = keyboard.bottomRect.height > 0 ? 0 : this.navigationInsetBottom;
    } catch (error) {
      this.releaseKeyboardInsets();
      throw error as Error;
    }
    this.windowPrepared = true;
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'window-insets',
      'top=' + this.chatInsetTop + ',bottom=' + this.chatInsetBottom);
  }

  private applyChatInsets(): void {
    if (!this.active || !this.windowPrepared) { return; }
    const script = `document.documentElement.style.setProperty('--chat-safe-top','${this.chatInsetTop}px');document.documentElement.style.setProperty('--chat-safe-bottom','${this.chatInsetBottom}px');`;
    this.controller.runJavaScript(script).catch(() => {});
  }

  private releaseKeyboardInsets(): void {
    const main = this.keyboardWindow;
    if (!main) { return; }
    this.keyboardWindow = undefined;
    try { main.off('keyboardHeightChange', this.keyboardHeightChanged); }
    catch (error) {
      const failure = error as BusinessError;
      ClientDiagnostics.event('SYSTEM', 'shared-client', 'keyboard-unsubscribe-failed',
        'errorCode=' + (typeof failure.code === 'number' ? failure.code : 0));
    }
  }

  private backPending: boolean = false;

  onBackPress(): boolean {
    if (!this.active || this.loadFailed || this.renderWaiting || !sharedWebAvailable()) { return false; }
    if (this.backPending) { return true; }
    this.backPending = true;
    const generation = this.probeGeneration;
    // onBackPress is synchronous in SDK26. Resolve the Web layer first, then
    // perform the normal root-page background action without destroying drafts
    // or native transfers. Never navigate ArkWeb's document history here.
    this.controller.runJavaScript('typeof window.__memohBack === "function" && window.__memohBack()')
      .then(async (handled: string) => {
        if (!this.active || generation !== this.probeGeneration) { return; }
        if (handled !== 'true') { await this.context.moveAbilityToBackground(); }
      }).catch(() => {
        ClientDiagnostics.event('SYSTEM', 'shared-client', 'back-dispatch-failed', '');
      }).finally(() => { this.backPending = false; });
    return true;
  }

  aboutToDisappear(): void {
    ImageTimingDump.detach();
    this.clearClientWait();
    this.cancelWebDownloads();
    this.active = false;
    this.releaseKeyboardInsets();
    this.probeGeneration++;
    this.probing = false;
    this.nativeReady = false;
    this.chatReady = false;
    this.nativeClient?.close(); this.nativeClient = undefined;
    this.photoSelection.close();
  }
  private attachNative(): void {
    void this.prepareWindow().then(() => this.applyChatInsets()).catch(() => { ClientDiagnostics.event('SYSTEM', 'window', 'layout-prepare-failed', ''); });
    if (!this.active) { return; }
    this.clearClientWait();
    this.loginReady = false;
    this.nativeReady = false;
    try {
      this.cancelWebDownloads();
      this.attachDownloads();
      this.nativeClient?.close();
      this.nativeClient = new NativeClient(this.controller, (state: string) => this.clientState(state),
        (theme: string) => this.setClientTheme(theme), this.context, (): void => this.webNetwork.reset());
      this.nativeReady = this.nativeClient.register();
      if (this.nativeReady) { this.webNetwork.attach(this.controller); }
      ImageTimingDump.bind(this.controller, (): boolean => this.active && this.chatReady && this.nativeReady);
      if (!this.nativeReady) { this.status = '附件功能暂时不可用，请重新打开聊天'; }
    } catch { this.nativeReady = false; this.status = '附件功能暂时不可用，请重新打开聊天'; }
  }
  private cancelWebDownloads(): void {
    this.webDownloads.forEach((item: webview.WebDownloadItem) => {
      try { item.cancel(); } catch { /* A completed download is no longer cancellable. */ }
      this.removeWebDownloadCache(item);
    });
    this.webDownloads.clear();
  }
  private webDownloadCache(item: webview.WebDownloadItem): string {
    return this.context.cacheDir + '/memoh-downloads/' + item.getGuid().replace(/[^A-Za-z0-9-]/g, '');
  }
  private removeWebDownloadCache(item: webview.WebDownloadItem): void {
    try { const path = this.webDownloadCache(item); if (fs.accessSync(path)) { fs.unlinkSync(path); } }
    catch { ClientDiagnostics.event('SYSTEM', 'web-download', 'cache-release-failed', ''); }
  }
  private async saveWebDownload(item: webview.WebDownloadItem, generation: number): Promise<void> {
    const source = this.webDownloadCache(item);
    try {
      if (!this.active || generation !== this.probeGeneration) { return; }
      const name = item.getSuggestedFileName().replace(/[\\/\x00-\x1f]/g, '_');
      const options = new picker.DocumentSaveOptions();
      options.newFileNames = [name];
      const uris = await new picker.DocumentViewPicker(this.context).save(options);
      if (!this.active || generation !== this.probeGeneration || !uris[0]) { return; }
      // Keep the system-granted document URI; its pathname alone does not carry the grant.
      const destination = await fs.open(uris[0], fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
      let copied = 0;
      try {
        await fs.copyFile(source, destination.fd);
        fs.fsyncSync(destination.fd);
        copied = fs.statSync(destination.fd).size;
        if (copied !== item.getReceivedBytes()) { throw new Error('MEDIA_DOWNLOAD_INCOMPLETE'); }
      } finally { await fs.close(destination.fd); }
      ClientDiagnostics.event('SYSTEM', 'web-download', 'save-complete', 'bytes=' + copied);
    } finally { this.removeWebDownloadCache(item); }
  }
  private attachDownloads(): void {
    const delegate = new webview.WebDownloadDelegate();
    this.downloadDelegate = delegate;
    delegate.onBeforeDownload((item: webview.WebDownloadItem) => {
      // The page exposes only authenticated, hash-verified original bytes as local Blob downloads.
      if (!this.active || this.loadFailed || this.webSource !== WEB_START ||
        !item.getUrl().startsWith('blob:begonia-client://app/')) {
        item.cancel();
        ClientDiagnostics.event('SYSTEM', 'web-download', 'source-rejected', '');
        return;
      }
      const name = item.getSuggestedFileName().replace(/[\\/\x00-\x1f]/g, '_');
      if (!name || name === '.' || name === '..') { item.cancel(); return; }
      const guid = item.getGuid();
      this.webDownloads.set(guid, item);
      try {
        const directory = this.context.cacheDir + '/memoh-downloads';
        if (!fs.accessSync(directory)) { fs.mkdirSync(directory); }
        item.start(this.webDownloadCache(item));
      }
      catch (error) {
        try { item.cancel(); } catch { /* Preserve the first save failure. */ }
        this.webDownloads.delete(guid);
        this.removeWebDownloadCache(item);
        ClientDiagnostics.failure('SYSTEM', 'web-download', 'download-failed', error as Error);
      }
    });
    delegate.onDownloadFinish((item: webview.WebDownloadItem) => {
      if (!this.webDownloads.has(item.getGuid())) { return; }
      this.webDownloads.delete(item.getGuid());
      ClientDiagnostics.event('SYSTEM', 'web-download', 'save-started', '');
      void this.saveWebDownload(item, this.probeGeneration).catch((error: Error) => {
        ClientDiagnostics.failure('SYSTEM', 'web-download', 'save-failed', error);
        this.getUIContext().getPromptAction().showToast({ message: '原件未保存，请重试' });
      });
    });
    delegate.onDownloadFailed((item: webview.WebDownloadItem) => {
      this.webDownloads.delete(item.getGuid());
      this.removeWebDownloadCache(item);
      ClientDiagnostics.event('SYSTEM', 'web-download', 'save-interrupted', 'code=' + item.getLastErrorCode());
    });
    this.controller.setDownloadDelegate(delegate);
  }
  private openExternal(url: string, userGesture: boolean): void {
    if (!this.active || !externalBrowserLink(url, userGesture)) { return; }
    const generation = this.probeGeneration;
    // Official AbilityKit default-browser Want; no second Web document receives the bridge.
    this.context.startAbility({ action: 'ohos.want.action.viewData', entities: ['entity.system.browsable'], uri: url })
      .then(() => { if (generation === this.probeGeneration) { this.status = '已交给系统浏览器打开'; } })
      .catch(() => { if (generation === this.probeGeneration) { this.status = '系统浏览器未能打开链接'; } });
  }
  private response(code: number, mime: string, data: ArrayBuffer): WebResourceResponse {
    const response = new WebResourceResponse();
    response.setResponseCode(code);
    response.setReasonMessage(code === 200 ? 'OK' : 'Blocked');
    response.setResponseMimeType(mime);
    response.setResponseEncoding(mime.startsWith('text/') || mime === 'application/javascript' || mime === 'application/json' ? 'utf-8' : '');
    response.setResponseHeader([{ headerKey: 'X-Content-Type-Options', headerValue: 'nosniff' },
      { headerKey: 'Content-Type', headerValue: mime }]);
    response.setResponseData(data);
    response.setResponseIsReady(true);
    return response;
  }
  private intercept(request: WebResourceRequest): WebResourceResponse | null {
    const url = request.getRequestUrl();
    const upload = /^begonia-client:\/\/app\/upload\/([a-f0-9]{32})$/.exec(url);
    if (upload && request.getRequestMethod() === 'GET' && !request.isMainFrame()) {
      try {
        if (!this.nativeClient || !this.active || !this.chatReady) { throw new Error('UPLOAD_IDENTITY'); }
        const original = this.nativeClient.uploadPreview(upload[1]);
        const response = new WebResourceResponse();
        response.setResponseCode(200); response.setReasonMessage('OK');
        response.setResponseMimeType(original.mime); response.setResponseEncoding('');
        response.setResponseHeader([{ headerKey: 'Cache-Control', headerValue: 'no-store' },
          { headerKey: 'X-Content-Type-Options', headerValue: 'nosniff' }]);
        response.setResponseData(original.fd); response.setResponseIsReady(true); return response;
      } catch { return this.response(403, 'text/plain', new ArrayBuffer(0)); }
    }
    const path = packagedPath(url);
    if (path && request.getRequestMethod() === 'GET') {
      if (path === 'index.html' && !request.isMainFrame()) { return this.response(403, 'text/plain', new ArrayBuffer(0)); }
      try {
        const bytes = this.context.resourceManager.getRawFileContentSync('client-web/' + path);
        const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
        if (bytes.byteLength !== WEB_RESOURCES[path].bytes) { return this.response(500, 'text/plain', new ArrayBuffer(0)); }
        return this.response(200, WEB_RESOURCES[path].mime, buffer);
      } catch { return this.response(500, 'text/plain', new ArrayBuffer(0)); }
    }
    if ((memohRequest(url) || viewerRequest(url)) && !request.isMainFrame()) { return null; }
    if (url.startsWith('blob:begonia-client://app/')) { return null; }
    return this.response(403, 'text/plain', new ArrayBuffer(0));
  }
  private pageEnded(url: string): void {
    if (!this.active) { return; }
    this.applyChatInsets();
    if (packagedPath(url) === 'index.html' && this.webSource === WEB_START && this.nativeReady && !this.loadFailed) {
      ClientDiagnostics.event('SYSTEM', 'shared-client', 'document-loaded', 'client initialization not yet confirmed');
      if (!this.chatReady && !this.loginReady && this.clientStage !== 'failed') { this.waitForClient(); }
    }
  }
  private clearClientWait(): void {
    if (this.clientTimer !== -1) { clearTimeout(this.clientTimer); this.clientTimer = -1; }
  }
  private waitForClient(): void {
    if (this.clientTimer !== -1) { return; }
    const generation = this.probeGeneration;
    this.clientTimer = setTimeout(() => {
      this.clientTimer = -1;
      if (!this.active || generation !== this.probeGeneration || this.chatReady || this.loginReady || this.loadFailed) { return; }
      this.status = '聊天加载超时，请重试';
      ClientDiagnostics.event('SYSTEM', 'shared-client', 'client-timeout', this.clientStage);
    }, 60000);
  }
  private clientState(state: string): void {
    if (!this.active || this.webSource !== WEB_START || !this.nativeReady || this.loadFailed) { return; }
    if (state === 'login-page' || state === 'failed') { this.cancelWebDownloads(); }
    this.chatReady = state === 'ready';
    this.loginReady = state === 'login-page';
    this.clientStage = state;
    if (this.chatReady || this.loginReady || state === 'failed') { this.clearClientWait(); } else { this.waitForClient(); }
    this.status = state === 'failed' ? '聊天初始化失败，请在页面中重试' : state === 'ready' ? '聊天已连接' : '正在恢复聊天会话…';
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'client-' + state, '');
  }
  private pageFailed(event: OnErrorReceiveEvent): void {
    if (!this.active) { return; }
    const requestUrl = event.request.getRequestUrl();
    if (!event.request.isMainFrame()) {
      if (memohRequest(requestUrl)) {
        ClientDiagnostics.event('SYSTEM', 'web-network', 'api-request-failed',
          'errorCode=' + event.error.getErrorCode());
      }
      return;
    }
    if (packagedPath(requestUrl) === '') { return; }
    this.cancelWebDownloads();
    this.probeGeneration++;
    this.clearClientWait(); this.loginReady = false;
    this.probing = false;
    this.chatReady = false;
    this.loadFailed = true;
    this.status = '聊天页面加载失败，请重试';
    this.webNetwork.reset();
    ClientDiagnostics.event('SYSTEM', 'arkweb-capability-gate', 'main-document-load-failed', 'errorCode=' + event.error.getErrorCode());
  }
  private renderExited(reason: RenderExitReason): void {
    if (!this.active) { return; }
    this.cancelWebDownloads();
    this.probeGeneration++;
    this.clearClientWait();
    this.probing = false; this.chatReady = false; this.loginReady = false; this.renderWaiting = false;
    this.loadFailed = true; this.nativeReady = false;
    this.nativeClient?.close(); this.nativeClient = undefined;
    this.status = '聊天页面运行中断，请重新打开聊天';
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'render-exited', 'reason=' + reason);
  }
  private renderNotResponding(data: RenderProcessNotRespondingData): void {
    if (!this.active || this.loadFailed || this.renderWaiting) { return; }
    this.renderWaiting = true;
    // The JavaScript stack may contain conversation or credential data; retain only SDK diagnostics.
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'render-not-responding', 'pid=' + data.pid + ';reason=' + data.reason);
  }
  private renderResponding(): void {
    if (!this.active || !this.renderWaiting || this.loadFailed) { return; }
    this.renderWaiting = false;
    ClientDiagnostics.event('SYSTEM', 'shared-client', 'render-responding', '');
  }
  private retry(): void {
    if (!this.active || this.probing) { return; }
    this.cancelWebDownloads();
    this.probeGeneration++;
    this.clearClientWait(); this.loginReady = false; this.clientStage = 'document';
    this.chatReady = false; this.renderWaiting = false;
    this.loadFailed = false;
    this.webSource = WEB_START;
    this.attachNative();
    try { this.controller.loadUrl(WEB_START); } catch { this.status = '聊天页面加载失败，请重试'; }
  }
  build() {
    Column() {
      if ((!this.chatReady && !this.loginReady) || this.renderWaiting) {
        Text(this.renderWaiting ? '聊天页面暂时无响应，请稍候或重新打开聊天' : this.status).fontSize(12).fontColor(this.textColor).width('100%').padding(12)
      }
      if (sharedWebAvailable()) {
        if ((!this.chatReady && !this.loginReady) || this.renderWaiting) {
          Button(this.probing ? '正在打开…' : '重新打开聊天').enabled(!this.probing)
            .onClick(() => { this.retry(); })
        }
        Web({ src: WEB_START, controller: this.controller })
        .darkMode(WebDarkMode.Auto).forceDarkAccess(false)
        .backgroundColor(this.surfaceColor)
        .javaScriptAccess(true).domStorageAccess(true).databaseAccess(true)
        .fileAccess(false).mixedMode(MixedMode.All).multiWindowAccess(true).allowWindowOpenMethod(false)
        .onContextMenuShow((event: OnContextMenuShowEvent) => {
          if (!this.active || !this.chatReady || this.webSource !== WEB_START ||
            event.param.getMediaType() !== ContextMenuMediaType.Image ||
            !event.param.getSourceUrl().startsWith('blob:begonia-client://app/')) { return false; }
          // ArkWeb's native Save As Image feeds the same verified Blob download delegate.
          event.result.saveImage();
          event.result.closeContextMenu();
          return true;
        })
        .onConsole((event: OnConsoleEvent) => {
          // Only fixed transport classifications leave ArkWeb; never URLs, headers or message text.
          const message = event.message.getMessage();
          const code = message.match(/net::(ERR_[A-Z_]+)/);
          if (code) { ClientDiagnostics.event('SYSTEM', 'web-network', 'transport-error', code[1]); }
          else if (/CORS|cross.origin/i.test(message)) { ClientDiagnostics.event('SYSTEM', 'web-network', 'cors-error', ''); }
          else if (/mixed content/i.test(message)) { ClientDiagnostics.event('SYSTEM', 'web-network', 'mixed-content', ''); }
          return true;
        })
        .onShowFileSelector((event: OnShowFileSelectorEvent) => {
          const generation = this.probeGeneration;
          const maximum = event.fileSelector.getMode() === FileSelectorMode.FileOpenMultipleMode ? 20 : 1;
          const accepted = event.fileSelector.getAcceptType();
          // ArkWeb may expand image/* to image extensions before this callback.
          const imageExtensions = ['.avif', '.bmp', '.gif', '.heic', '.heif', '.hevc', '.ico', '.jfif', '.jpe', '.jpeg',
            '.jpg', '.pjp', '.pjpeg', '.png', '.apng', '.svg', '.svgz', '.tif', '.tiff', '.webp', '.xbm', '.dng'];
          const photosOnly = accepted.length > 0 && accepted.every((type: string) =>
            type.trim().toLowerCase().startsWith('image/') || imageExtensions.includes(type.trim().toLowerCase()));
          ClientDiagnostics.event('SYSTEM', 'file-selector', 'opened', 'maxSelectNumber=' + maximum + '/photos=' + photosOnly +
            '/accept=' + accepted.join(',').replace(/[^a-zA-Z0-9/.*,+-]/g, ''));
          this.photoSelection.select(maximum, photosOnly).then((uris: string[]) => {
              ClientDiagnostics.event('SYSTEM', 'file-selector', 'selected', 'count=' + uris.length);
            event.result.handleFileList(this.active && generation === this.probeGeneration ? uris : []);
            }).catch((error: Error) => {
              ClientDiagnostics.failure('SYSTEM', 'file-selector', 'selection-failed', error);
              event.result.handleFileList([]);
            });
          return true;
        })
        .onControllerAttached(() => { this.attachNative(); })
        .onInterceptRequest((event: OnInterceptRequestEvent) => this.intercept(event.request))
        .onLoadIntercept((event: OnLoadInterceptEvent) => {
          if (packagedPath(event.data.getRequestUrl()) !== '') { return false; }
          if (!event.data.isMainFrame() && viewerRequest(event.data.getRequestUrl())) { return false; }
          this.openExternal(event.data.getRequestUrl(), event.data.isMainFrame() && event.data.isRequestGesture() &&
            !event.data.isRedirect() && event.data.getRequestMethod() === 'GET');
          return true;
        })
        .onWindowNew((event: OnWindowNewEvent) => {
          // Required even when declining the popup, otherwise ArkWeb rendering can block.
          event.handler.setWebController(null);
          this.openExternal(event.targetUrl, event.isUserTrigger);
        })
        .onPageEnd((event: OnPageEndEvent) => { this.pageEnded(event.url); })
        .onErrorReceive((event: OnErrorReceiveEvent) => { this.pageFailed(event); })
        .onRenderExited((event: OnRenderExitedEvent) => { this.renderExited(event.renderExitReason); })
        .onRenderProcessNotResponding((event: RenderProcessNotRespondingData) => { this.renderNotResponding(event); })
        .onRenderProcessResponding(() => { this.renderResponding(); })
        .layoutWeight(1).width('100%')
      } else {
        Text('聊天页面暂时不可用，请重新打开应用').fontSize(14)
      }
    }.width('100%').height('100%').backgroundColor(this.surfaceColor)
  }
}
