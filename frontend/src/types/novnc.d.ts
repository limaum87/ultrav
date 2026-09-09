// Minimal ambient typings for @novnc/novnc (the package ships none).
declare module '@novnc/novnc' {
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string, options?: {
      credentials?: { password?: string; username?: string; target?: string };
      shared?: boolean;
    });
    scaleViewport: boolean;
    resizeSession: boolean;
    sendCredentials(credentials: { password?: string; username?: string; target?: string }): void;
    disconnect(): void;
    sendCtrlAltDel(): void;
    focus(): void;
    blur(): void;
  }
}
