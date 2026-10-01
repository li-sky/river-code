import Picker, { Categories, Theme, EmojiStyle, SuggestionMode } from "emoji-picker-react";
import zh from "emoji-picker-react/dist/data/emojis-zh";

export default function EmojiPicker({ onSelect }: { onSelect: (emoji: string) => void }) {
  return (
    <div className="emoji-picker">
      <Picker
        emojiData={zh}
        theme={Theme.DARK}
        emojiStyle={EmojiStyle.NATIVE}
        suggestedEmojisMode={SuggestionMode.RECENT}
        width="100%"
        height="min(420px, 55dvh)"
        autoFocusSearch={false}
        searchPlaceholder="搜索表情"
        searchClearButtonLabel="清除搜索"
        previewConfig={{ showPreview: false }}
        categories={[
          { category: Categories.SUGGESTED, name: "最近使用" },
          { category: Categories.SMILEYS_PEOPLE, name: "表情与人物" },
          { category: Categories.ANIMALS_NATURE, name: "动物与自然" },
          { category: Categories.FOOD_DRINK, name: "食物与饮品" },
          { category: Categories.TRAVEL_PLACES, name: "旅行与地点" },
          { category: Categories.ACTIVITIES, name: "活动" },
          { category: Categories.OBJECTS, name: "物品" },
          { category: Categories.SYMBOLS, name: "符号" },
          { category: Categories.FLAGS, name: "旗帜" },
        ]}
        onEmojiClick={({ emoji }, event) => {
          // Preserve the exact displayed sequence: the library callback can
          // normalize mixed skin tones to the first matching variation.
          const unified = event.target instanceof Element
            ? event.target.closest<HTMLButtonElement>("button[data-unified]")?.dataset.unified
            : undefined;
          onSelect(unified
            ? String.fromCodePoint(...unified.split("-").map((code) => parseInt(code, 16)))
            : emoji);
        }}
      />
    </div>
  );
}
