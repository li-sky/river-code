import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import * as Dialog from "@radix-ui/react-dialog";
import * as Switch from "@radix-ui/react-switch";
import {
  ArrowLeft,
  ArrowRight,
  AlertCircle,
  Check,
  Clock3,
  CircleHelp,
  Crown,
  Github,
  Headphones,
  LogOut,
  LoaderCircle,
  MessageCircle,
  Mic,
  MicOff,
  Pin,
  PinOff,
  Plus,
  Send,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Smile,
  Spade,
  Users,
  Volume2,
  VolumeX,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import { api, post } from "./lib/api";
import type {
  Config,
  Player,
  RoomSettings,
  RoomState,
  RoomSummary,
  User,
  WSMessage,
} from "./lib/types";
import { playSound, setSoundSettings, unlockAudio } from "./lib/sound";
import { useVoice } from "./lib/voice";
const defaultRoom: RoomSettings = {
  visibility: "public",
  smallBlind: 10,
  bigBlind: 20,
  buyIn: 2000,
  maxPlayers: 6,
  actionSeconds: 30,
  voiceEnabled: true,
  voiceMode: "free",
  spectatorVoiceEnabled: false,
  chatEnabled: true,
  reactionsEnabled: true,
};
const fmt = (n: number) => n.toLocaleString("zh-CN");
const phases: Record<string, string> = {
  preflop: "翻牌前",
  flop: "翻牌",
  turn: "转牌",
  river: "河牌",
  showdown: "摊牌",
  complete: "本手结束",
};
function seatPosition(index: number, count: number, mobile = false) {
  // Nine seats need wider paired rows on narrow screens to keep names readable.
  const portraitNine = [
    [50, 94],
    [94, 84],
    [102, 54],
    [94, 30],
    [76, 6],
    [24, 6],
    [6, 30],
    [-2, 54],
    [6, 84],
  ];
  if (mobile && count === 9) {
    const [x, y] = portraitNine[index];
    return { x, y };
  }
  const angle = (index / count) * Math.PI * 2;
  return {
    x: 50 + Math.sin(angle) * 36,
    y: 50 + Math.cos(angle) * 43,
  };
}
const EmojiPicker = lazy(() => import("./components/EmojiPicker"));
function EmojiChoices({
  disabledReason,
  onSelect,
}: {
  disabledReason?: string;
  onSelect: (emoji: string) => void;
}) {
  if (disabledReason)
    return <p className="muted" role="status">{disabledReason}</p>;
  return (
    <Suspense fallback={<p className="muted" role="status">正在加载表情…</p>}>
      <EmojiPicker onSelect={onSelect} />
    </Suspense>
  );
}
function Button({
  children,
  onClick,
  disabled = false,
  className = "",
  type = "button",
}: {
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  className?: string;
  type?: "button" | "submit";
}) {
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={"button " + className}
    >
      {children}
    </button>
  );
}
function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  restoreFocus = false,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  restoreFocus?: boolean;
}) {
  const opener = useRef<HTMLElement | null>(null);
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="modal-overlay" />
        <Dialog.Content
          className="modal"
          onOpenAutoFocus={() => {
            if (restoreFocus)
              opener.current = document.activeElement as HTMLElement;
          }}
          onCloseAutoFocus={(event) => {
            if (restoreFocus && opener.current?.isConnected) {
              event.preventDefault();
              opener.current.focus();
            }
          }}
        >
          <div className="modal-heading">
            <div>
              <Dialog.Title>{title}</Dialog.Title>
              <Dialog.Description className={description ? "muted" : "sr-only"}>
                {description || title}
              </Dialog.Description>
            </div>
            <Dialog.Close className="icon-button" aria-label="关闭">
              <X size={20} />
            </Dialog.Close>
          </div>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
function Toggle({
  label,
  description,
  value,
  onChange,
  disabled,
}: {
  label: string;
  description?: string;
  value: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label className="toggle-row">
      <span>
        {label}
        {description && <small>{description}</small>}
      </span>
      <Switch.Root
        className="switch"
        aria-label={label}
        checked={value}
        onCheckedChange={onChange}
        disabled={disabled}
      >
        <Switch.Thumb className="switch-thumb" />
      </Switch.Root>
    </label>
  );
}
function Avatar({
  name,
  url,
  size = "",
  emoji,
}: {
  name: string;
  url?: string;
  size?: string;
  emoji?: string;
}) {
  return (
    <span className={"avatar " + size}>
      <span>{name.slice(0, 1).toUpperCase()}</span>
      {url && (
        <img
          src={url}
          alt={name}
          onError={(e) => {
            e.currentTarget.style.display = "none";
          }}
        />
      )}
      {emoji && <span className="avatar-emoji">{emoji}</span>}
    </span>
  );
}
function Card({
  code,
  back = false,
  small = false,
  index = 0,
}: {
  code?: string;
  back?: boolean;
  small?: boolean;
  index?: number;
}) {
  const rank = code?.slice(0, -1) || "",
    suit = code?.slice(-1) || "",
    glyph = ({ s: "♠", h: "♥", d: "♦", c: "♣" } as Record<string, string>)[
      suit
    ];
  return (
    <div
      style={{ animationDelay: `${index * 85}ms` }}
      className={
        "card " +
        (back ? "card-back " : code ? "card-face " : "card-empty ") +
        (small ? "card-small " : "") +
        (["h", "d"].includes(suit) ? "red" : "")
      }
      aria-label={back ? "背面牌" : code ? `${rank}${glyph}` : "等待发牌"}
    >
      {back ? (
        <div className="card-pattern">
          <Spade />
        </div>
      ) : code ? (
        <>
          <span className="card-corner">
            {rank}
            <b>{glyph}</b>
          </span>
          <span className="card-suit">{glyph}</span>
          <span className="card-corner card-bottom">
            {rank}
            <b>{glyph}</b>
          </span>
        </>
      ) : (
        <Spade size={22} />
      )}
    </div>
  );
}
function SettingsFields({
  value,
  onChange,
  config,
}: {
  value: RoomSettings;
  onChange: (s: RoomSettings) => void;
  config: Config;
}) {
  const number = (
    key: keyof RoomSettings,
    label: string,
    min: number,
    max: number,
  ) => (
    <label className="field">
      {label}
      <input
        type="number"
        min={min}
        max={max}
        required
        value={String(value[key])}
        onChange={(e) => onChange({ ...value, [key]: Number(e.target.value) })}
      />
    </label>
  );
  return (
    <>
      <label className="toggle-row room-visibility-field">
        <span>
          房间可见性
          <small>
            {value.visibility === "private"
              ? "不在大厅显示，持邀请链接的用户登录后可加入"
              : "在大厅显示，站内用户都可以加入"}
          </small>
        </span>
        <select
          className="room-select"
          aria-label="房间可见性"
          value={value.visibility}
          onChange={(e) =>
            onChange({
              ...value,
              visibility: e.target.value as RoomSettings["visibility"],
            })
          }
        >
          <option value="public">公开房间</option>
          <option value="private">私人房间</option>
        </select>
      </label>
      <div className="form-grid">
        {number("smallBlind", "小盲注", 1, 500000)}
        {number("bigBlind", "大盲注", value.smallBlind * 2, 1000000)}
        {number("buyIn", "默认买入筹码", value.bigBlind * 2, 1000000000)}
        {number("maxPlayers", "座位数", 2, 9)}
        {number("actionSeconds", "每次行动时间（秒）", 10, 120)}
      </div>
      <div className="toggle-list">
        <Toggle
          label="语音聊天"
          value={value.voiceEnabled}
          disabled={!config.voiceEnabled}
          onChange={(v) => onChange({ ...value, voiceEnabled: v })}
        />
        <label className="toggle-row">
          <span>
            默认语音模式<small>玩家可临时切换自己的说话方式</small>
          </span>
          <select
            className="room-select"
            aria-label="默认语音模式"
            value={value.voiceMode || config.defaultVoiceMode || "free"}
            disabled={!config.voiceEnabled || !value.voiceEnabled}
            onChange={(e) =>
              onChange({
                ...value,
                voiceMode: e.target.value as "free" | "push-to-talk",
              })
            }
          >
            <option value="free">自由说话</option>
            <option value="push-to-talk">按住说话</option>
          </select>
        </label>
        <Toggle
          label="允许旁观者加入语音"
          description="最多 9 人同时参与，优先为入座玩家保留位置"
          value={value.spectatorVoiceEnabled || false}
          disabled={
            !config.voiceEnabled ||
            !config.spectatorVoiceEnabled ||
            !value.voiceEnabled
          }
          onChange={(v) => onChange({ ...value, spectatorVoiceEnabled: v })}
        />
        <Toggle
          label="文字聊天"
          value={value.chatEnabled}
          disabled={!config.chatEnabled}
          onChange={(v) => onChange({ ...value, chatEnabled: v })}
        />
        <Toggle
          label="Emoji 互动"
          value={value.reactionsEnabled}
          disabled={!config.reactionsEnabled}
          onChange={(v) => onChange({ ...value, reactionsEnabled: v })}
        />
      </div>
    </>
  );
}
export default function App() {
  const [config, setConfig] = useState<Config | null>(null),
    [user, setUser] = useState<User | null>(null),
    [ready, setReady] = useState(false),
    [rooms, setRooms] = useState<RoomSummary[]>([]),
    [roomId, setRoomId] = useState(
      () => new URLSearchParams(location.search).get("room") || "",
    ),
    [profile, setProfile] = useState(false),
    [create, setCreate] = useState(false),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [name, setName] = useState(""),
    [roomSettings, setRoomSettings] = useState(defaultRoom),
    [authMode, setAuthMode] = useState<"guest" | "login" | "register">("guest");
  const report = useCallback(
    (e: unknown) => setError(e instanceof Error ? e.message : String(e)),
    [],
  );
  useEffect(() => {
    Promise.all([api<Config>("/config"), api<User>("/me").catch(() => null)])
      .then(([c, u]) => {
        setConfig(c);
        setRoomSettings((r) => ({
          ...r,
          voiceMode: c.defaultVoiceMode || "free",
        }));
        setUser(u);
        if (!c.guestEnabled) setAuthMode("login");
      })
      .catch(report)
      .finally(() => setReady(true));
  }, [report]);
  useEffect(() => {
    if (!user || roomId) return;
    let active = true;
    const refresh = () =>
      api<RoomSummary[]>("/rooms")
        .then((r) => {
          if (active) setRooms(r);
        })
        .catch(report);
    void refresh();
    const timer = setInterval(refresh, 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [user, roomId, report]);
  useEffect(() => {
    if (user)
      setSoundSettings(user.settings.soundEnabled, user.settings.volume);
  }, [user]);
  useEffect(() => {
    const handler = () =>
      setRoomId(new URLSearchParams(location.search).get("room") || "");
    window.addEventListener("popstate", handler);
    return () => window.removeEventListener("popstate", handler);
  }, []);
  const goRoom = (id: string) => {
    setRoomId(id);
    history.pushState({}, "", id ? `/?room=${encodeURIComponent(id)}` : "/");
  };
  const run = async (fn: () => Promise<void>) => {
    setError("");
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      report(e);
    } finally {
      setBusy(false);
    }
  };
  const logout = () =>
    run(async () => {
      await post("/auth/logout");
      setUser(null);
      goRoom("");
    });
  if (!ready || !config)
    return (
      <div className="loading-page">
        <Spade className="loading-spade" />
        <b>RIVER</b>
        <span>{error || "正在准备牌桌…"}</span>
        {error && <Button onClick={() => location.reload()}>重新连接</Button>}
      </div>
    );
  return (
    <div
      className="app"
      onPointerDown={() => {
        void unlockAudio();
      }}
    >
      <header className="topbar">
        <a
          className="brand"
          href="/"
          onClick={(e) => {
            e.preventDefault();
            goRoom("");
          }}
        >
          <span className="brand-mark">
            <Spade size={23} fill="currentColor" />
          </span>
          <span>
            RIVER<small>POKER WITH FRIENDS</small>
          </span>
        </a>
        <div className="header-right">
          <span className="environment">
            <span className="live-dot" />
            私享德州
          </span>
          {user ? (
            <>
              <button
                className="profile-trigger"
                aria-label={`${user.name} 的个人设置`}
                onClick={() => setProfile(true)}
              >
                <Avatar name={user.name} url={user.avatarUrl} />
                <span>{user.name}</span>
                <Settings size={16} />
              </button>
              <button
                className="icon-button logout"
                onClick={logout}
                aria-label="退出登录"
              >
                <LogOut size={18} />
              </button>
            </>
          ) : (
            <span className="header-label">朋友相聚，好牌开场。</span>
          )}
        </div>
      </header>
      {error && (
        <div role="alert" className="toast">
          <span>{error}</span>
          <button aria-label="关闭提示" onClick={() => setError("")}>
            <X size={17} />
          </button>
        </div>
      )}
      {!user ? (
        <main className="welcome">
          <div className="welcome-copy">
            <div className="eyebrow">
              <span />
              YOUR TABLE. YOUR PEOPLE.
            </div>
            <h1>
              好朋友。
              <br />
              好牌局。
            </h1>
            <p>
              给熟悉的人留一个座位。
              <br />
              属于你们的无限注德州扑克，随时开场。
            </p>
            <div className="welcome-perks">
              <span>
                <ShieldCheck size={17} /> 私有部署
              </span>
              <span>
                <Mic size={17} /> 实时语音
              </span>
              <span>
                <Users size={17} /> 多人对战
              </span>
            </div>
            <div className="welcome-art" aria-hidden="true">
              <Card code="As" />
              <Card code="Kh" />
              <div className="art-chip">
                R<small>RIVER</small>
              </div>
            </div>
          </div>
          <section className="auth-panel">
            <div className="eyebrow">TAKE YOUR SEAT</div>
            <h2>入座，开始今晚的牌局</h2>
            <p className="muted">筹码只用于娱乐，好时光才是真正的收获。</p>
            <div className="segmented" role="group" aria-label="入座方式">
              {config.guestEnabled && (
                <button
                  className={authMode === "guest" ? "active" : ""}
                  aria-pressed={authMode === "guest"}
                  onClick={() => setAuthMode("guest")}
                >
                  访客
                </button>
              )}
              <button
                className={authMode === "login" ? "active" : ""}
                aria-pressed={authMode === "login"}
                onClick={() => setAuthMode("login")}
              >
                登录
              </button>
              <button
                className={authMode === "register" ? "active" : ""}
                aria-pressed={authMode === "register"}
                onClick={() => setAuthMode("register")}
              >
                注册
              </button>
            </div>
            <form
              key={authMode}
              onSubmit={(e) => {
                e.preventDefault();
                const f = new FormData(e.currentTarget);
                void run(async () => {
                  const body =
                    authMode === "guest"
                      ? { name: f.get("name"), password: f.get("joinPassword") }
                      : authMode === "login"
                        ? {
                            email: f.get("email"),
                            password: f.get("password"),
                            joinPassword: f.get("joinPassword"),
                          }
                        : {
                            email: f.get("email"),
                            password: f.get("password"),
                            name: f.get("name"),
                            joinPassword: f.get("joinPassword"),
                          };
                  await post("/auth/" + authMode, body);
                  setUser(await api<User>("/me"));
                  setConfig(await api<Config>("/config"));
                  void unlockAudio();
                });
              }}
            >
              {authMode !== "login" && (
                <label className="field">
                  你的昵称
                  <input
                    name="name"
                    placeholder="牌桌上的名字"
                    maxLength={30}
                    required
                    autoComplete="nickname"
                  />
                </label>
              )}
              {authMode !== "guest" && (
                <>
                  <label className="field">
                    邮箱
                    <input
                      name="email"
                      type="email"
                      placeholder="you@example.com"
                      required
                      autoComplete="email"
                    />
                  </label>
                  <label className="field">
                    账号密码
                    <input
                      name="password"
                      type="password"
                      minLength={authMode === "register" ? 8 : 1}
                      placeholder={
                        authMode === "register" ? "至少 8 位" : "输入账号密码"
                      }
                      required
                      autoComplete={
                        authMode === "register"
                          ? "new-password"
                          : "current-password"
                      }
                    />
                  </label>
                </>
              )}
              {config.passwordRequired && (
                <label className="field">
                  全站加入密码
                  <input
                    name="joinPassword"
                    type="password"
                    placeholder="向房主获取加入密码"
                    required
                  />
                </label>
              )}
              <Button type="submit" className="primary full" disabled={busy}>
                {busy
                  ? "正在入座…"
                  : authMode === "guest"
                    ? "以访客身份进入"
                    : authMode === "register"
                      ? "创建账号"
                      : "登录"}
                <ArrowRight size={18} />
              </Button>
            </form>
            {config.githubEnabled && (
              <>
                <div className="divider">
                  <span>或使用账号</span>
                </div>
                <Button
                  className="github full"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      if (config.passwordRequired) {
                        const input = document.querySelector<HTMLInputElement>(
                          'input[name="joinPassword"]',
                        );
                        const password = input?.value || "";
                        if (!password) throw new Error("请先输入全站加入密码");
                        await post("/auth/access", { password });
                      }
                      location.href = "/api/auth/github";
                    })
                  }
                >
                  <Github size={18} />
                  通过 GitHub 登录
                </Button>
              </>
            )}
            <div className="auth-footnote">
              <Spade size={13} /> NO-LIMIT TEXAS HOLD’EM
            </div>
          </section>
        </main>
      ) : roomId ? (
        <Room
          key={roomId}
          roomId={roomId}
          user={user}
          config={config}
          onBack={() => goRoom("")}
          onError={report}
          onUser={setUser}
        />
      ) : (
        <main className="lobby">
          <div className="lobby-hero">
            <div>
              <div className="eyebrow">
                <span /> THE LOBBY
              </div>
              <h1>今晚，在哪一桌？</h1>
              <p>加入朋友的牌局，或为他们开一张新桌。</p>
            </div>
            <Button className="primary" onClick={() => setCreate(true)}>
              <Plus size={19} />
              创建牌桌
            </Button>
          </div>
          <div className="lobby-info">
            <span>
              <span className="live-dot" />
              {rooms.length} 张公开牌桌
            </span>
            <span>
              <Spade size={15} />
              无限注德州扑克
            </span>
            <span>
              <CircleHelp size={15} />
              现金桌 · 娱乐筹码
            </span>
          </div>
          <div className="section-heading">
            <h2>
              公开牌桌 <span>{String(rooms.length).padStart(2, "0")}</span>
            </h2>
            <span>邀请朋友，一起入座</span>
          </div>
          {rooms.length ? (
            <div className="room-grid">
              {rooms.map((r, i) => (
                <button
                  className="room-card"
                  key={r.id}
                  onClick={() => goRoom(r.id)}
                >
                  <div className="room-card-top">
                    <span className="room-number">
                      TABLE {String(i + 1).padStart(2, "0")}
                    </span>
                    <span
                      className={
                        "status-tag " +
                        (r.status === "playing" ? "playing" : "")
                      }
                    >
                      {r.status === "playing" ? "牌局进行中" : "等待入座"}
                    </span>
                  </div>
                  <div className="mini-table" aria-hidden="true">
                    <div className="mini-board">
                      <Card code="As" small />
                      <Card code="Kh" small />
                      <Card back small />
                    </div>
                    <div className="mini-seat seat-a" />
                    <div className="mini-seat seat-b" />
                    <div className="mini-seat seat-c" />
                  </div>
                  <h3>{r.name}</h3>
                  <div className="room-stats">
                    <span>
                      盲注{" "}
                      <b>
                        {fmt(r.settings.smallBlind)} /{" "}
                        {fmt(r.settings.bigBlind)}
                      </b>
                    </span>
                    <span>
                      <Users size={15} />
                      {r.players} / {r.settings.maxPlayers}
                    </span>
                  </div>
                  <div className="room-enter">
                    <span>买入 {fmt(r.settings.buyIn)}</span>
                    <span>
                      进入牌桌 <ArrowRight size={17} />
                    </span>
                  </div>
                </button>
              ))}
            </div>
          ) : (
            <div className="empty-lobby">
              <div className="empty-illustration">
                <div className="empty-ring">
                  <Spade size={38} />
                </div>
                <span className="empty-star">✧</span>
              </div>
              <h3>好牌局，从第一张桌开始</h3>
              <p>
                现在还没有公开的牌桌。创建你的牌桌，
                <br />
                把链接发给朋友，今晚的故事就此开始。
              </p>
              <Button onClick={() => setCreate(true)} className="primary">
                <Plus size={18} />
                创建第一张牌桌
              </Button>
              <div className="empty-features">
                <span>
                  <Users size={16} /> 2 – 9 人
                </span>
                <span>
                  <Mic size={16} /> 语音畅聊
                </span>
                <span>
                  <Smile size={16} /> 表情互动
                </span>
              </div>
            </div>
          )}
          <div className="lobby-footer">
            <span>
              <Spade size={14} /> RIVER · 让每次相聚都有一手好牌
            </span>
            <span>PRIVATE TABLES. SHARED MOMENTS.</span>
          </div>
        </main>
      )}
      <Modal
        open={create}
        onOpenChange={setCreate}
        title="创建你的牌桌"
        description="设置今晚的节奏，邀请朋友一起入座。"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              const r = await post<{ id: string }>("/rooms", {
                name,
                settings: roomSettings,
              });
              setCreate(false);
              goRoom(r.id);
            });
          }}
        >
          <label className="field">
            牌桌名称
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={50}
              placeholder="例如：周五朋友局"
              required
            />
          </label>
          <SettingsFields
            value={roomSettings}
            onChange={setRoomSettings}
            config={config}
          />
          <Button type="submit" className="primary full" disabled={busy}>
            创建牌桌
            <ArrowRight size={17} />
          </Button>
        </form>
      </Modal>
      {user && (
        <Profile
          open={profile}
          onOpenChange={setProfile}
          user={user}
          onUser={setUser}
          onError={report}
        />
      )}
    </div>
  );
}
function Profile({
  open,
  onOpenChange,
  user,
  onUser,
  onError,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  user: User;
  onUser: (u: User) => void;
  onError: (e: unknown) => void;
}) {
  const [name, setName] = useState(user.name),
    [email, setEmail] = useState(""),
    [sound, setSound] = useState(user.settings.soundEnabled),
    [volume, setVolume] = useState(user.settings.volume),
    [saving, setSaving] = useState(false);
  useEffect(() => {
    if (open) {
      setName(user.name);
      setSound(user.settings.soundEnabled);
      setVolume(user.settings.volume);
    }
  }, [open, user]);
  const save = async () => {
    setSaving(true);
    try {
      await api("/me", {
        method: "PATCH",
        body: JSON.stringify({
          name,
          ...(email ? { gravatarEmail: email } : {}),
          settings: { soundEnabled: sound, volume },
        }),
      });
      onUser(await api<User>("/me"));
      onOpenChange(false);
    } catch (e) {
      onError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal
      open={open}
      onOpenChange={(v) => {
        if (!v)
          setSoundSettings(user.settings.soundEnabled, user.settings.volume);
        onOpenChange(v);
      }}
      title="个人设置"
      description="你的头像、名字和声音偏好。"
    >
      <div className="profile-avatar">
        <Avatar name={user.name} url={user.avatarUrl} size="large" />
        <label className="button secondary upload">
          上传头像
          <input
            type="file"
            accept="image/png,image/jpeg,image/gif"
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (!f) return;
              const data = new FormData();
              data.append("file", f);
              try {
                await api("/me/avatar", { method: "POST", body: data });
                onUser(await api<User>("/me"));
              } catch (err) {
                onError(err);
              }
            }}
          />
        </label>
        <span className="muted">PNG、JPG 或 GIF</span>
      </div>
      <label className="field">
        昵称
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={30}
          required
        />
      </label>
      <label className="field">
        Gravatar 邮箱
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="输入邮箱以使用 Gravatar 头像"
        />
      </label>
      <div className="toggle-list">
        <Toggle
          label="游戏音效"
          description="发牌、筹码、弃牌与获胜音效"
          value={sound}
          onChange={(v) => {
            setSound(v);
            setSoundSettings(v, volume);
          }}
        />
        <label className="volume-label">
          <Volume2 size={17} />
          <input
            aria-label="音效音量"
            type="range"
            min="0"
            max="1"
            step="0.01"
            value={volume}
            onChange={(e) => {
              const v = Number(e.target.value);
              setVolume(v);
              setSoundSettings(sound, v);
            }}
          />
          <span>{Math.round(volume * 100)}%</span>
        </label>
      </div>
      <Button
        className="primary full"
        onClick={() => void save()}
        disabled={saving || !name.trim()}
      >
        {saving ? "保存中…" : "保存设置"}
        <Check size={17} />
      </Button>
    </Modal>
  );
}
function Room({
  roomId,
  user,
  config,
  onBack,
  onError,
  onUser,
}: {
  roomId: string;
  user: User;
  config: Config;
  onBack: () => void;
  onError: (e: unknown) => void;
  onUser: (u: User) => void;
}) {
  const [state, setState] = useState<RoomState | null>(null),
    [connected, setConnected] = useState(false),
    [fatal, setFatal] = useState(false),
    [chatOpen, setChatOpen] = useState(false),
    [settingsOpen, setSettingsOpen] = useState(false),
    [rulesOpen, setRulesOpen] = useState(false),
    [inviteOpen, setInviteOpen] = useState(false),
    [editedSettings, setEditedSettings] = useState<RoomSettings>(defaultRoom),
    [chat, setChat] = useState(""),
    [raise, setRaise] = useState(0),
    [submittingToken, setSubmittingToken] = useState(""),
    [now, setNow] = useState(Date.now()),
    [target, setTarget] = useState<Player | null>(null),
    [selfEmoji, setSelfEmoji] = useState(false),
    [seat, setSeat] = useState<number | null>(null),
    [buyIn, setBuyIn] = useState(2000),
    [stackTarget, setStackTarget] = useState<Player | null>(null),
    [stackAmount, setStackAmount] = useState(0),
    [copied, setCopied] = useState(false),
    [bubbles, setBubbles] = useState<Record<string, string>>({}),
    [reactions, setReactions] = useState<
      { id: number; from: string; to: string; emoji: string }[]
    >([]);
  const latestState = useRef<RoomState | null>(null),
    pendingToken = useRef(""),
    ws = useRef<WebSocket | null>(null),
    handler = useRef<(m: WSMessage) => void>(() => {}),
    chatEnd = useRef<HTMLDivElement>(null),
    tableArea = useRef<HTMLDivElement>(null),
    bubbleTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const send = useCallback(
    (message: Record<string, unknown>) => {
      if (ws.current?.readyState === WebSocket.OPEN)
        ws.current.send(JSON.stringify(message));
      else onError(new Error("连接正在恢复，请稍候"));
    },
    [onError],
  );
  const sendSignal = useCallback(
    (to: string, data: unknown) => send({ type: "signal", to, data }),
    [send],
  );
  const voice = useVoice({
    roomId,
    enabled: Boolean(
      config.voiceEnabled &&
      state?.settings.voiceEnabled &&
      state.voiceParticipantIds?.includes(user.id),
    ),
    muted: user.settings.voiceMuted,
    mode: state?.settings.voiceMode || config.defaultVoiceMode || "free",
    localUserId: user.id,
    players: (state?.players || []).filter(
      (p) => p.connected && state?.voiceParticipantIds?.includes(p.id),
    ),
    sendSignal,
    iceServers: config.iceServers,
  });
  const bubble = (id: string, text: string) => {
    clearTimeout(bubbleTimers.current[id]);
    setBubbles((b) => ({ ...b, [id]: text }));
    bubbleTimers.current[id] = setTimeout(
      () =>
        setBubbles((b) => {
          const n = { ...b };
          delete n[id];
          return n;
        }),
      5000,
    );
  };
  handler.current = (message) => {
    if (message.type === "state") {
      const old = latestState.current;
      if (old) {
        const previous = new Set(old.messages.map((m) => m.id));
        message.state.messages
          .filter((m) => !previous.has(m.id))
          .forEach((m) => bubble(m.userId, m.text));
        message.state.players
          .filter(
            (p) =>
              p.avatarEmoji &&
              p.avatarEmoji !==
                old.players.find((o) => o.id === p.id)?.avatarEmoji,
          )
          .forEach((p) => bubble(p.id, p.avatarEmoji));
      }
      latestState.current = message.state;
      if (message.state.hand?.turnToken !== pendingToken.current) {
        setSubmittingToken("");
      }
      setState(message.state);
    } else if (message.type === "error") {
      pendingToken.current = "";
      setSubmittingToken("");
      onError(new Error(message.message));
    } else if (message.type === "sound") playSound(message.sound);
    else if (message.type === "signal")
      voice.handleSignal(message.from, message.data);
    else if (message.type === "reaction") {
      const item = { ...message, id: Date.now() + Math.random() };
      setReactions((r) => [...r, item]);
      setTimeout(
        () => setReactions((r) => r.filter((x) => x.id !== item.id)),
        1800,
      );
      playSound("reaction");
    }
  };
  useEffect(() => {
    let disposed = false,
      retry: ReturnType<typeof setTimeout>,
      attempt = 0;
    const connect = () => {
      const socket = new WebSocket(
        `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/rooms/${encodeURIComponent(roomId)}/ws`,
      );
      ws.current = socket;
      socket.onopen = () => {
        if (disposed) {
          socket.close();
          return;
        }
        attempt = 0;
        setConnected(true);
      };
      socket.onmessage = (e) => {
        if (disposed) return;
        try {
          handler.current(JSON.parse(e.data));
        } catch {
          onError(new Error("收到无效的服务器消息"));
        }
      };
      socket.onclose = (e) => {
        if (disposed) return;
        setConnected(false);
        if (e.code === 4003) {
          onError(new Error(e.reason || "房主已将你移出牌桌"));
          onBack();
          return;
        }
        if (!disposed)
          retry = setTimeout(connect, Math.min(1000 * 2 ** attempt++, 10000));
      };
      socket.onerror = () => setConnected(false);
    };
    api<RoomState>("/rooms/" + encodeURIComponent(roomId))
      .then((s) => {
        if (!disposed) {
          latestState.current = s;
          setState(s);
          connect();
        }
      })
      .catch((e) => {
        if (!disposed) {
          onError(e);
          setFatal(true);
        }
      });
    const timer = setInterval(() => setNow(Date.now()), 250);
    return () => {
      disposed = true;
      clearTimeout(retry);
      clearInterval(timer);
      ws.current?.close();
      Object.values(bubbleTimers.current).forEach(clearTimeout);
    };
  }, [roomId, onError, user.name, user.avatarUrl]);
  useEffect(() => {
    chatEnd.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }, [state?.messages.length, chatOpen]);
  const hand = state?.hand,
    me = state?.players.find((p) => p.id === user.id),
    myHand = hand?.players.find((p) => p.id === user.id),
    isHost = state?.hostId === user.id,
    active = Boolean(hand && !["complete", "showdown"].includes(hand.phase)),
    myTurn = Boolean(
      active && me && myHand && !myHand.folded && hand?.turnSeat === me.seat,
    ),
    toCall = Math.max(0, (hand?.currentBet || 0) - (myHand?.bet || 0)),
    minRaise =
      (hand?.currentBet || 0) +
      (hand?.minRaise || state?.settings.bigBlind || 20),
    maxRaise = (me?.stack || 0) + (myHand?.bet || 0),
    seconds = hand
      ? Math.min(
          state?.settings.actionSeconds || 120,
          Math.max(0, Math.ceil((new Date(hand.deadline).getTime() - now) / 1000)),
        )
      : 0;
  useEffect(() => {
    setRaise(Math.min(maxRaise, minRaise));
  }, [minRaise, maxRaise, hand?.phase]);
  if (!state)
    return (
      <main className="room-loading">
        <Spade className="loading-spade" />
        <h2>{fatal ? "无法进入这张牌桌" : "正在连接牌桌…"}</h2>
        {fatal && (
          <Button onClick={onBack}>
            <ArrowLeft size={17} />
            返回大厅
          </Button>
        )}
      </main>
    );
  const seated = state.players.filter((p) => p.seat >= 0),
    watchers = state.players.filter((p) => p.seat < 0),
    actingPlayer = active
      ? state.players.find((p) => p.seat === hand?.turnSeat)
      : undefined,
    submitting = Boolean(hand?.turnToken && submittingToken === hand.turnToken),
    pinnedMessage = state.pinnedMessage,
    allowedReactions =
      config.reactionsEnabled && state.settings.reactionsEnabled,
    allowedChat = config.chatEnabled && state.settings.chatEnabled;
  const leave = () => {
    send({ type: "leave" });
    voice.leave();
    onBack();
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(location.href);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      setInviteOpen(true);
    }
  };
  const action = (action: string, amount?: number) => {
    if (
      !connected ||
      ws.current?.readyState !== WebSocket.OPEN ||
      !hand?.turnToken ||
      pendingToken.current === hand.turnToken
    )
      return;
    pendingToken.current = hand.turnToken;
    setSubmittingToken(hand.turnToken);
    send({
      type: "action",
      action,
      turnToken: hand.turnToken,
      ...(amount !== undefined ? { amount } : {}),
    });
  };
  const submitChat = () => {
    if (!chat.trim()) return;
    send({ type: "chat", text: chat.trim() });
    setChat("");
  };
  const saveMute = async () => {
    voice.toggleMute();
    try {
      await api("/me", {
        method: "PATCH",
        body: JSON.stringify({
          settings: { voiceMuted: !voice.muted },
        }),
      });
      onUser(await api<User>("/me"));
    } catch (e) {
      onError(e);
    }
  };
  return (
    <main className="room-page">
      <div className="room-toolbar">
        <div className="room-title">
          <button className="icon-button" onClick={leave} aria-label="返回大厅">
            <ArrowLeft size={20} />
          </button>
          <div>
            <h1>
              {state.name}
              {isHost && (
                <span className="host-label">
                  <Crown size={12} /> HOST
                </span>
              )}
            </h1>
            <div className="room-subtitle">
              <span>NL HOLD’EM</span>
              <span>
                {state.settings.visibility === "private"
                  ? "私人房间"
                  : "公开房间"}
              </span>
              <span>
                盲注 {fmt(state.settings.smallBlind)} /{" "}
                {fmt(state.settings.bigBlind)}
              </span>
              <span className={connected ? "connection" : "connection offline"}>
                {connected ? <Wifi size={12} /> : <WifiOff size={12} />}{" "}
                {connected ? "已连接" : "重新连接中"}
              </span>
            </div>
          </div>
        </div>
        <div className="toolbar-actions">
          <Button
            className="secondary invite-button"
            onClick={() => void copy()}
          >
            {copied ? <Check size={15} /> : <Users size={15} />}
            <span>{copied ? "链接已复制" : "邀请朋友"}</span>
          </Button>
          <button
            className="icon-button"
            onClick={() => setRulesOpen(true)}
            aria-label="玩法说明"
          >
            <CircleHelp size={19} />
          </button>
          {isHost && (
            <button
              className="icon-button"
              onClick={() => {
                setEditedSettings(state.settings);
                setSettingsOpen(true);
              }}
              aria-label="房间设置"
            >
              <SlidersHorizontal size={19} />
            </button>
          )}
        </div>
      </div>
      <div className={"game-layout " + (chatOpen ? "with-chat" : "")}>
        <section className="game-main">
          <div className="table-status">
            <span className="live-dot" />
            <span>
              {hand
                ? `第 ${hand.number} 手 · ${phases[hand.phase]}`
                : "等待开局"}
            </span>
            {actingPlayer && (
              <span className="acting-player">
                <Clock3 size={16} />
                {actingPlayer.id === user.id
                  ? "轮到你行动"
                  : `${actingPlayer.name} 行动中`}
              </span>
            )}
            <span className="table-status-right">
              <Users size={14} />
              {seated.length}/{state.settings.maxPlayers} 入座
              {watchers.length > 0 && ` · ${watchers.length} 旁观`}
            </span>
          </div>
          <div
            className={
              "table-area " + (state.settings.maxPlayers === 9 ? "nine-seats" : "")
            }
          >
            <div className="poker-table" ref={tableArea}>
              <div className="table-rail" />
              <div className="felt">
                <div className="felt-wordmark">
                  <Spade size={21} />
                  <b>RIVER</b>
                  <span>NO-LIMIT HOLD’EM</span>
                </div>
                <div className="community">
                  <div className="pot">
                    <span>底池</span>
                    <strong>{fmt(hand?.pot || 0)}</strong>
                    <div className="pot-chips">
                      <i />
                      <i />
                      <i />
                    </div>
                  </div>
                  <div className="board">
                    {Array.from({ length: 5 }, (_, i) => (
                      <Card
                        key={`${hand?.number}-${i}-${hand?.board[i] || "empty"}`}
                        code={hand?.board[i]}
                        index={i}
                      />
                    ))}
                  </div>
                  <div className="hand-caption">
                    {hand?.phase === "complete"
                      ? hand.winners
                          ?.map(
                            (w) =>
                              `${state.players.find((p) => p.id === w.id)?.name || "玩家"} +${fmt(w.amount)} · ${w.description}`,
                          )
                          .join(" / ")
                      : hand
                        ? phases[hand.phase]
                        : "入座后，由房主开始牌局"}
                  </div>
                </div>
              </div>
              {Array.from({ length: state.settings.maxPlayers }, (_, i) => {
                const p = state.players.find((p) => p.seat === i),
                  hp = hand?.players.find((p) => p.seat === i),
                  visualIndex =
                    (i -
                      (me && me.seat >= 0 ? me.seat : 0) +
                      state.settings.maxPlayers) %
                    state.settings.maxPlayers,
                  position = seatPosition(
                    visualIndex,
                    state.settings.maxPlayers,
                  ),
                  portraitPosition = seatPosition(
                    visualIndex,
                    state.settings.maxPlayers,
                    true,
                  );
                const turn = active && hand?.turnSeat === i,
                  winner =
                    hand?.phase === "complete" &&
                    hand.winners?.some((w) => w.id === p?.id);
                return (
                  <div
                    key={i}
                    className={
                      "table-seat seat-index-" +
                      visualIndex +
                      (position.x > 50.1 ? " side-right" : "") +
                      (p ? " occupied" : "") +
                      (p?.id === user.id ? " own-seat" : "") +
                      (turn ? " current-turn" : "") +
                      (hp?.folded ? " folded" : "") +
                      (winner ? " winner" : "")
                    }
                    style={
                      {
                        "--seat-x": `${position.x}%`,
                        "--seat-y": `${position.y}%`,
                        "--seat-mobile-x": `${portraitPosition.x}%`,
                        "--seat-mobile-y": `${portraitPosition.y}%`,
                      } as React.CSSProperties
                    }
                  >
                    {p ? (
                      <>
                        <div
                          className={
                            "seat-cards " +
                            (hp?.cards.length ? "revealed-cards" : "")
                          }
                        >
                          {hp &&
                            !hp.folded &&
                            (hp.cards.length
                              ? hp.cards
                              : [undefined, undefined]
                            ).map((c, j) => (
                              <Card
                                key={j}
                                code={c}
                                back={!c}
                                small
                                index={j}
                              />
                            ))}
                        </div>
                        {bubbles[p.id] && (
                          <div className="thought-bubble" key={bubbles[p.id]}>
                            {bubbles[p.id]}
                          </div>
                        )}
                        <button
                          className="seat-avatar-button"
                          onClick={() =>
                            p.id === user.id ? setSelfEmoji(true) : setTarget(p)
                          }
                          aria-label={`${p.name} 的互动菜单`}
                        >
                          <Avatar
                            name={p.name}
                            url={p.avatarUrl}
                            emoji={p.avatarEmoji}
                          />
                          {turn && (
                            <span
                              className={
                                "turn-timer " + (seconds <= 5 ? "urgent" : "")
                              }
                            >
                              {seconds}s
                            </span>
                          )}
                          {winner && (
                            <span className="winner-crown">
                              <Crown size={15} />
                            </span>
                          )}
                        </button>
                        <div className="seat-name">
                          {p.name}
                          {p.id === user.id && <small>你</small>}
                          {p.id === state.hostId && <Crown size={11} />}
                        </div>
                        <div className="seat-stack">
                          {fmt(p.stack)}
                          {!p.connected && <WifiOff size={10} />}
                        </div>
                        {hand?.dealerSeat === i && (
                          <span className="dealer-button" aria-label="庄家">
                            D
                          </span>
                        )}
                        {hp && (hp.folded || hp.allIn) && (
                          <span className="player-badge">
                            {hp.folded ? "弃牌" : "ALL IN"}
                          </span>
                        )}
                        {hp && hp.bet > 0 && (
                          <div className="seat-bet">
                            <i className="chip" />
                            {fmt(hp.bet)}
                          </div>
                        )}
                      </>
                    ) : (
                      <button
                        className="empty-seat"
                        disabled={
                          Boolean(me && me.seat >= 0) || active || !connected
                        }
                        onClick={() => {
                          setSeat(i);
                          setBuyIn(state.settings.buyIn);
                        }}
                        aria-label={`坐入 ${i + 1} 号座位`}
                      >
                        <Plus size={20} />
                        <span>{i + 1} 号座位</span>
                      </button>
                    )}
                  </div>
                );
              })}
              {reactions.map((r) => {
                const from = state.players.find((p) => p.id === r.from),
                  to = state.players.find((p) => p.id === r.to);
                if (!from || !to) return null;
                const position = (seat: number) => {
                  if (seat < 0) return { x: 50, y: 3 };
                  const index =
                    (seat -
                      (me && me.seat >= 0 ? me.seat : 0) +
                      state.settings.maxPlayers) %
                    state.settings.maxPlayers;
                  return seatPosition(
                    index,
                    state.settings.maxPlayers,
                    window.innerWidth <= 600,
                  );
                };
                const a = position(from.seat),
                  b = position(to.seat);
                return (
                  <span
                    className="flying-emoji"
                    key={r.id}
                    style={
                      {
                        left: `${a.x}%`,
                        top: `${a.y}%`,
                        "--flight-x": `${((b.x - a.x) * (tableArea.current?.clientWidth || 700)) / 100}px`,
                        "--flight-y": `${((b.y - a.y) * (tableArea.current?.clientHeight || 400)) / 100}px`,
                      } as React.CSSProperties
                    }
                  >
                    {r.emoji}
                  </span>
                );
              })}
            </div>
          </div>
          <div className="under-table">
            <div className="under-table-left">
              <span>
                <ShieldCheck size={14} /> 服务器公正发牌
              </span>
              <span>无限注 · 现金桌</span>
            </div>
            <span>娱乐筹码，无现金价值</span>
          </div>
          <section className={"action-dock " + (myTurn ? "your-turn" : "")}>
            <div className="action-summary">
              {myHand?.cards.length === 2 && (
                <div className="mobile-hole-cards">
                  <span>你的底牌</span>
                  <div>
                    {myHand.cards.map((c, i) => (
                      <Card
                        key={`${hand?.number}-${c}`}
                        code={c}
                        small
                        index={i}
                      />
                    ))}
                  </div>
                </div>
              )}
              <div className="action-heading">
                <span className="eyebrow">
                  {myTurn
                    ? "轮到你行动"
                    : me && me.seat >= 0
                      ? "已入座"
                      : "正在旁观"}
                </span>
                <h3>
                  {myTurn
                    ? `轮到你了${toCall ? ` · 跟注 ${fmt(Math.min(toCall, me?.stack || 0))}` : " · 可以过牌"}`
                    : active
                      ? hpStatus(myHand, hand?.turnSeat === me?.seat)
                      : me && me.seat >= 0
                        ? "准备好下一手？"
                        : "选一个座位，加入牌局"}
                </h3>
              </div>
              <div className="dock-meta">
                {me && me.seat >= 0 ? (
                  <>
                    <span>
                      你的筹码 <strong>{fmt(me.stack)}</strong>
                    </span>
                    <Button
                      className="text-button"
                      disabled={active || !connected}
                      onClick={() => send({ type: "stand" })}
                    >
                      离座
                    </Button>
                  </>
                ) : (
                  <span>
                    {state.settings.maxPlayers - seated.length} 个空位
                  </span>
                )}
              </div>
            </div>
            {myTurn && (
              <div className={"action-clock " + (seconds <= 5 ? "urgent" : "")}>
                <span>
                  {seconds <= 5 ? (
                    <AlertCircle size={16} />
                  ) : (
                    <Clock3 size={16} />
                  )}
                  {seconds <= 5 ? "即将超时" : "行动剩余时间"}
                </span>
                <div
                  className="action-clock-track"
                  role="progressbar"
                  aria-label="行动剩余时间"
                  aria-valuemin={0}
                  aria-valuemax={state.settings.actionSeconds}
                  aria-valuenow={Math.min(
                    seconds,
                    state.settings.actionSeconds,
                  )}
                  aria-valuetext={`${seconds} 秒`}
                >
                  <i
                    style={{
                      width: `${Math.min(100, (seconds / state.settings.actionSeconds) * 100)}%`,
                    }}
                  />
                </div>
                <strong>
                  {seconds}
                  <small> 秒</small>
                </strong>
              </div>
            )}
            {myTurn ? (
              <div className="bet-controls" aria-busy={submitting}>
                {myHand?.canRaise && maxRaise >= minRaise && (
                  <fieldset
                    className="raise-controls"
                    disabled={!connected || submitting}
                  >
                    <legend>加注到的本轮总额</legend>
                    <div className="raise-presets">
                      {[2, 3, 4].map((n) => (
                        <button
                          key={n}
                          onClick={() =>
                            setRaise(
                              Math.min(
                                maxRaise,
                                Math.max(
                                  minRaise,
                                  (hand?.currentBet || 0) +
                                    state.settings.bigBlind * n,
                                ),
                              ),
                            )
                          }
                        >
                          {n}× BB
                        </button>
                      ))}
                      <button
                        onClick={() =>
                          setRaise(
                            Math.min(
                              maxRaise,
                              Math.max(
                                minRaise,
                                Math.round(
                                  (hand?.currentBet || 0) +
                                    (hand?.pot || 0) / 2,
                                ),
                              ),
                            ),
                          )
                        }
                      >
                        ½ 底池
                      </button>
                    </div>
                    <input
                      aria-label="加注筹码滑块"
                      type="range"
                      min={minRaise}
                      max={maxRaise}
                      step={1}
                      value={raise}
                      onChange={(e) => setRaise(Number(e.target.value))}
                    />
                    <label className="raise-amount">
                      <span>加注至</span>
                      <input
                        className="raise-input"
                        aria-label="加注到的总金额"
                        type="number"
                        inputMode="numeric"
                        step={1}
                        min={minRaise}
                        max={maxRaise}
                        value={raise}
                        onChange={(e) => setRaise(Number(e.target.value))}
                      />
                    </label>
                    <p className="raise-hint">
                      本轮累计下注 · 最低 {fmt(minRaise)} · 最高 {fmt(maxRaise)}
                    </p>
                  </fieldset>
                )}
                <div className="bet-actions">
                  <Button
                    className="fold-button"
                    disabled={!connected || submitting}
                    onClick={() => action("fold")}
                  >
                    弃牌
                  </Button>
                  <Button
                    className="check-button"
                    disabled={!connected || submitting}
                    onClick={() => action(toCall ? "call" : "check")}
                  >
                    {toCall
                      ? `跟注 ${fmt(Math.min(toCall, me?.stack || 0))}`
                      : "过牌"}
                  </Button>
                  <Button
                    className="primary"
                    disabled={
                      !connected ||
                      submitting ||
                      !myHand?.canRaise ||
                      maxRaise <= hand!.currentBet ||
                      raise < minRaise ||
                      raise > maxRaise ||
                      !Number.isInteger(raise)
                    }
                    onClick={() => action("raise", raise)}
                  >
                    加注至 {fmt(raise)}
                  </Button>
                  <Button
                    className="allin-button"
                    disabled={
                      !connected ||
                      submitting ||
                      !me?.stack ||
                      (maxRaise > hand!.currentBet && !myHand?.canRaise)
                    }
                    onClick={() => action("allin")}
                  >
                    <span>
                      ALL IN<small>全下 {fmt(me?.stack || 0)}</small>
                    </span>
                  </Button>
                </div>
                {submitting && (
                  <p className="action-feedback" role="status">
                    <LoaderCircle size={16} /> 已发送，等待牌桌更新…
                  </p>
                )}
              </div>
            ) : (
              <div className="waiting-actions">
                {isHost && !active && (
                  <Button
                    className="primary"
                    disabled={
                      seated.filter((p) => p.stack > 0 && !p.sittingOut)
                        .length < 2 || !connected
                    }
                    onClick={() => send({ type: "start" })}
                  >
                    <Spade size={16} />
                    {hand ? "开始下一手" : "开始牌局"}
                  </Button>
                )}
                <span className="muted">
                  {!active && seated.length < 2
                    ? "至少需要 2 位玩家入座"
                    : !active && !isHost
                      ? "等待房主开始牌局"
                      : active
                        ? "每一手好牌，都值得耐心。"
                        : "盲注与筹码可在两手之间调整。"}
                </span>
                {me && me.seat >= 0 && allowedReactions && (
                  <button
                    className="icon-button"
                    onClick={() => setSelfEmoji(true)}
                    aria-label="设置头像表情"
                  >
                    <Smile size={20} />
                  </button>
                )}
              </div>
            )}
          </section>
          <div className="social-bar">
            <div className="voice-controls">
              {config.voiceEnabled && state.settings.voiceEnabled ? (
                <>
                  <button
                    className={
                      "voice-button " + (voice.joined ? "voice-joined" : "")
                    }
                    disabled={
                      !connected ||
                      !state.voiceParticipantIds?.includes(user.id)
                    }
                    title={
                      state.voiceParticipantIds?.includes(user.id)
                        ? "加入牌桌语音"
                        : (me && me.seat >= 0) ||
                            (config.spectatorVoiceEnabled &&
                              state.settings.spectatorVoiceEnabled)
                          ? "语音最多允许 9 人参与"
                          : "入座后可加入语音"
                    }
                    onClick={() => {
                      if (!voice.joined) void voice.join();
                      else void saveMute();
                    }}
                  >
                    <span>
                      {voice.joined && !voice.muted ? (
                        <Mic size={16} />
                      ) : (
                        <MicOff size={16} />
                      )}
                    </span>
                    {voice.joined
                      ? voice.muted
                        ? "麦克风已静音"
                        : "语音已连接"
                      : state.voiceParticipantIds?.includes(user.id)
                        ? "加入语音"
                        : (me && me.seat >= 0) ||
                            (config.spectatorVoiceEnabled &&
                              state.settings.spectatorVoiceEnabled)
                          ? "语音已满（9 人）"
                          : "入座后语音"}
                  </button>
                  {voice.joined && (
                    <>
                      <button
                        className="icon-button"
                        onClick={voice.toggleDeafen}
                        aria-label={
                          voice.deafened ? "打开语音播放" : "关闭语音播放"
                        }
                      >
                        <Headphones
                          size={17}
                          style={{ opacity: voice.deafened ? 0.4 : 1 }}
                        />
                      </button>
                      <select
                        aria-label="语音模式"
                        className="voice-mode"
                        value={voice.mode}
                        onChange={(e) =>
                          voice.setMode(
                            e.target.value as "free" | "push-to-talk",
                          )
                        }
                      >
                        <option value="free">自由说话</option>
                        <option value="push-to-talk">按住说话</option>
                      </select>
                      {voice.mode === "push-to-talk" && (
                        <button
                          className="push-talk"
                          onPointerDown={(e) => {
                            e.currentTarget.setPointerCapture(e.pointerId);
                            if (voice.muted) voice.toggleMute();
                            voice.pressToTalk(true);
                          }}
                          onPointerUp={() => voice.pressToTalk(false)}
                          onPointerCancel={() => voice.pressToTalk(false)}
                          onLostPointerCapture={() => voice.pressToTalk(false)}
                        >
                          按住说话
                        </button>
                      )}
                      <button className="text-button" onClick={voice.leave}>
                        退出
                      </button>
                    </>
                  )}
                </>
              ) : (
                <span className="muted">
                  <MicOff size={14} /> 语音已关闭
                </span>
              )}
            </div>
            <div className="social-actions">
              <button
                className="icon-button"
                aria-label={
                  user.settings.soundEnabled ? "关闭音效" : "打开音效"
                }
                onClick={async () => {
                  try {
                    await api("/me", {
                      method: "PATCH",
                      body: JSON.stringify({
                        settings: {
                          soundEnabled: !user.settings.soundEnabled,
                        },
                      }),
                    });
                    onUser(await api<User>("/me"));
                  } catch (e) {
                    onError(e);
                  }
                }}
              >
                {user.settings.soundEnabled ? (
                  <Volume2 size={18} />
                ) : (
                  <VolumeX size={18} />
                )}
              </button>
              {allowedChat && (
                <button
                  className={"chat-toggle " + (chatOpen ? "active" : "")}
                  aria-label="牌桌聊天"
                  aria-expanded={chatOpen}
                  aria-controls="table-chat"
                  onClick={() => setChatOpen(!chatOpen)}
                >
                  <MessageCircle size={17} />
                  <span>牌桌聊天</span>
                  {state.messages.length > 0 && (
                    <b>{Math.min(99, state.messages.length)}</b>
                  )}
                </button>
              )}
            </div>
          </div>
          {voice.error && (
            <div className="voice-error" role="alert">
              {voice.error}
              <button className="text-button" onClick={voice.resumeAudio}>
                重试播放
              </button>
            </div>
          )}
        </section>
        {chatOpen && allowedChat && (
          <aside className="chat-panel" id="table-chat" aria-label="牌桌聊天">
            <div className="chat-heading">
              <h3>
                <MessageCircle size={17} />
                牌桌聊天
              </h3>
              <button
                className="icon-button"
                onClick={() => setChatOpen(false)}
                aria-label="关闭聊天"
              >
                <X size={17} />
              </button>
            </div>
            {pinnedMessage && (
              <section className="pinned-message" aria-label="置顶消息">
                <div className="pinned-message-heading">
                  <span>
                    <Pin size={14} /> 置顶消息
                  </span>
                  {isHost && (
                    <button
                      type="button"
                      className="chat-pin-button"
                      disabled={!connected}
                      aria-label="取消置顶消息"
                      onClick={() => send({
                        type: "unpin_message",
                        messageId: pinnedMessage.id,
                      })}
                    >
                      <PinOff size={14} />取消置顶
                    </button>
                  )}
                </div>
                <div className="pinned-message-content">
                  <div className="pinned-message-author">
                    <span>{pinnedMessage.name}</span>
                    <time dateTime={pinnedMessage.at}>
                      {new Date(pinnedMessage.at).toLocaleTimeString("zh-CN", {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </time>
                  </div>
                  <p>{pinnedMessage.text}</p>
                </div>
              </section>
            )}
            <div className="chat-messages">
              {state.messages.length ? (
                state.messages.map((m) => (
                  <div
                    className={
                      "chat-message " +
                      (m.userId === user.id ? "own-message" : "")
                    }
                    key={m.id}
                  >
                    <span>
                      {m.name}
                      <time>
                        {new Date(m.at).toLocaleTimeString("zh-CN", {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </time>
                    </span>
                    <p>{m.text}</p>
                    {isHost && (
                      <button
                        type="button"
                        className="chat-pin-button"
                        disabled={!connected}
                        aria-label={pinnedMessage?.id === m.id ? "取消置顶此消息" : "置顶此消息"}
                        onClick={() => send({
                          type: pinnedMessage?.id === m.id ? "unpin_message" : "pin_message",
                          messageId: m.id,
                        })}
                      >
                        {pinnedMessage?.id === m.id ? <PinOff size={13} /> : <Pin size={13} />}
                        {pinnedMessage?.id === m.id ? "取消置顶" : "置顶"}
                      </button>
                    )}
                  </div>
                ))
              ) : (
                <div className="empty-chat">
                  <MessageCircle size={30} />
                  <p>一声招呼，牌局更有温度。</p>
                  <span>消息也会在头像气泡中出现</span>
                </div>
              )}
              <div ref={chatEnd} />
            </div>
            <form
              className="chat-form"
              onSubmit={(e) => {
                e.preventDefault();
                submitChat();
              }}
            >
              <input
                placeholder="和朋友说点什么…"
                aria-label="聊天消息"
                value={chat}
                onChange={(e) => setChat(e.target.value)}
                maxLength={300}
              />
              <button
                disabled={!chat.trim() || !connected}
                aria-label="发送消息"
              >
                <Send size={17} />
              </button>
            </form>
            <span className="chat-hint">Enter 发送 · 文明交流</span>
          </aside>
        )}
      </div>
      <Modal
        open={seat !== null}
        onOpenChange={(v) => {
          if (!v) setSeat(null);
        }}
        title="准备入座"
        description={`坐入 ${(seat ?? 0) + 1} 号座位，选择你的买入筹码。`}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            send({ type: "sit", seat, buyIn });
            setSeat(null);
          }}
        >
          <label className="field">
            买入筹码
            <input
              type="number"
              required
              min={state.settings.bigBlind * 2}
              max={1000000000}
              value={buyIn}
              onChange={(e) => setBuyIn(Number(e.target.value))}
            />
          </label>
          <p className="muted small-text">
            娱乐筹码只用于这张牌桌。房主可在两手之间调整筹码。
          </p>
          <Button
            className="primary full"
            type="submit"
            disabled={!connected || buyIn < state.settings.bigBlind * 2}
          >
            确认入座
            <ArrowRight size={17} />
          </Button>
        </form>
      </Modal>
      <Modal
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
        title="房间设置"
        description="盲注、筹码和座位设置在两手之间调整。"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            send({ type: "settings", settings: editedSettings });
            setSettingsOpen(false);
          }}
        >
          <SettingsFields
            value={editedSettings}
            onChange={setEditedSettings}
            config={config}
          />
          {active && (
            <p className="warning">当前牌局进行中，请在本手结束后保存。</p>
          )}
          <Button
            type="submit"
            className="primary full"
            disabled={active || !connected}
          >
            保存房间设置
          </Button>
        </form>
        <div className="stack-settings">
          <h3>玩家筹码</h3>
          {seated.map((p) => (
            <div key={p.id}>
              <span>{p.name}</span>
              <b>{fmt(p.stack)}</b>
              <button
                className="text-button"
                disabled={active}
                onClick={() => {
                  setStackTarget(p);
                  setStackAmount(p.stack);
                }}
              >
                调整
              </button>
              {p.id !== user.id && (
                <button
                  className="text-button"
                  disabled={active || !connected}
                  onClick={() => send({ type: "kick", playerId: p.id })}
                >
                  移出
                </button>
              )}
            </div>
          ))}
        </div>
      </Modal>
      <Modal
        open={stackTarget !== null}
        onOpenChange={(v) => {
          if (!v) setStackTarget(null);
        }}
        title={`调整 ${stackTarget?.name || ""} 的筹码`}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            send({
              type: "stack",
              playerId: stackTarget?.id,
              amount: stackAmount,
            });
            setStackTarget(null);
          }}
        >
          <label className="field">
            筹码总额
            <input
              type="number"
              min={0}
              max={1000000000}
              value={stackAmount}
              onChange={(e) => setStackAmount(Number(e.target.value))}
              required
            />
          </label>
          <Button
            className="primary full"
            type="submit"
            disabled={active || !connected}
          >
            确认调整
          </Button>
        </form>
      </Modal>
      <Modal
        open={target !== null}
        onOpenChange={(v) => {
          if (!v) setTarget(null);
        }}
        title={`向 ${target?.name || ""} 发射表情`}
        restoreFocus
        description="一点互动，让牌桌更热闹。"
      >
        <EmojiChoices
          disabledReason={
            !allowedReactions ? "这张牌桌已关闭 Emoji 互动。"
              : !connected ? "连接恢复后即可发送表情。" : undefined
          }
          onSelect={(emoji) => {
            if (!allowedReactions || !connected || !target) return;
            send({ type: "reaction", to: target.id, emoji });
            setTarget(null);
          }}
        />
      </Modal>
      <Modal
        open={selfEmoji}
        onOpenChange={setSelfEmoji}
        title="此刻的心情"
        restoreFocus
        description="选一个表情挂在头像旁，让大家知道你的想法。"
      >
        <EmojiChoices
          disabledReason={
            !allowedReactions ? "这张牌桌已关闭 Emoji 互动。"
              : !connected ? "连接恢复后即可发送表情。" : undefined
          }
          onSelect={(emoji) => {
            if (!allowedReactions || !connected) return;
            send({ type: "emoji", emoji });
            bubble(user.id, emoji);
            setSelfEmoji(false);
          }}
        />
        <Button
          className="secondary full"
          disabled={!allowedReactions || !connected}
          onClick={() => {
            send({ type: "emoji", emoji: "" });
            setSelfEmoji(false);
          }}
        >
          清除头像表情
        </Button>
      </Modal>
      <Modal
        open={rulesOpen}
        onOpenChange={setRulesOpen}
        title="无限注德州扑克"
        description="用你的两张底牌与五张公共牌，组成最强的五张牌。"
      >
        <div className="rules-copy">
          <p>
            每手依次进行翻牌前、翻牌、转牌、河牌四轮下注。你可以弃牌、过牌、跟注、加注，或把全部筹码推入底池。
          </p>
          <p>
            加注金额是本轮下注总额。最小加注由上一笔完整加注决定；不足最小加注时仍可
            All-in。
          </p>
          <p>
            牌型由强到弱：同花顺、四条、葫芦、同花、顺子、三条、两对、一对、高牌。平局平分底池，全下时自动计算边池。
          </p>
          <p>
            超时：可过牌时自动过牌，否则弃牌。断线后可以重新加入继续牌局。房主在本手结束后开始下一手。
          </p>
          <p className="muted">本系统使用娱乐筹码，筹码无现金价值。</p>
        </div>
      </Modal>
      <Modal
        open={inviteOpen}
        onOpenChange={setInviteOpen}
        title="邀请朋友"
        description="把牌桌链接发给朋友，他们登录后就能加入。"
      >
        <input
          className="invite-link"
          readOnly
          value={location.href}
          onFocus={(e) => e.currentTarget.select()}
        />
      </Modal>
    </main>
  );
}
function hpStatus(
  hp: { folded: boolean; allIn: boolean } | undefined,
  myTurn: boolean,
) {
  return hp?.folded
    ? "你已弃牌 · 等待下一手"
    : hp?.allIn
      ? "你已 All-in · 等待结果"
      : myTurn
        ? "轮到你了"
        : "等待其他玩家行动";
}
