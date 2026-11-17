export type Span = [number, number];

export type Stream = {
  index: number;
  codec_type: string;
  codec_name: string;
  profile: string;
  width: number;
  height: number;
  pix_fmt: string;
  avg_frame_rate: string;
  sample_rate: string;
  channels: number;
  channel_layout: string;
  color_transfer: string;
  color_primaries: string;
  bit_rate: string;
  tags: Record<string, string> | null;
  disposition: Record<string, number> | null;
};

export type Info = {
  streams: Stream[];
  format: {
    filename: string;
    format_name: string;
    duration: string;
    size: string;
    bit_rate: string;
  };
};

export type Options = {
  videoTrack: number;
  videoCodec: string;
  encoder: string;
  audioTrack: number;
  audioCodec: string;
  subtitleTrack: number;
  subtitleMode: string;
};

export type Transcode = {
  generation: number;
  state: string;
  options: Options;
  processed: number;
  available: number;
  speed: string;
  error: string;
  output?: Info;
  bytes: number;
};

export type Member = {
  id: string;
  name: string;
  address: string;
  joined: boolean;
  connected: boolean;
  position: number;
  buffered: Span[] | null;
  state: string;
  epoch: number;
};

export type Room = {
  source: "original" | "transcoded";
  position: number;
  playing: boolean;
  wantPlay: boolean;
  reason: string;
  epoch: number;
  revision: number;
  changedBy: string;
  serverTime: number;
  members: Member[];
  transcode: Transcode;
};

export type Initial = {
  source: Info;
  duration: number;
  capabilities: {
    encoders: string[];
    textBurn: boolean;
    toneMap: boolean;
  };
  room: Room;
};
