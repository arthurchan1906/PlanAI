import { useState, useRef, useCallback } from "react";
import { AutoComplete, Input, Space, Tag } from "antd";
import { SearchOutlined } from "@ant-design/icons";
import { api } from "../utils/api";

const TYPE_CONFIG = {
  task: { color: "blue", label: "Task", view: "tasks" },
  commit: { color: "cyan", label: "Commit", view: "commits" },
  bug: { color: "red", label: "Bug", view: "bugs" },
  decision: { color: "gold", label: "Decision", view: "decisions" },
  idea: { color: "purple", label: "Idea", view: "ideas" },
};

export default function GlobalSearch({ onNavigate }) {
  const [options, setOptions] = useState([]);
  const [value, setValue] = useState("");
  const timerRef = useRef(null);

  const handleSearch = useCallback((text) => {
    setValue(text);
    if (timerRef.current) clearTimeout(timerRef.current);
    if (!text || text.length < 2) {
      setOptions([]);
      return;
    }
    timerRef.current = setTimeout(async () => {
      try {
        const data = await api(`/pmai/search?q=${encodeURIComponent(text)}`);
        setOptions(
          (data.results || []).map((item, idx) => {
            const cfg = TYPE_CONFIG[item.type] || { color: "default", label: item.type };
            return {
              value: `${item.type}:${item.id}`,
              label: (
                <div className="search-result-item" key={idx}>
                  <Space>
                    <Tag color={cfg.color} style={{ fontSize: 10 }}>{cfg.label}</Tag>
                    <span>{item.title}</span>
                  </Space>
                  {item.severity && <Tag style={{ fontSize: 10 }}>{item.severity}</Tag>}
                  {item.status && <Tag style={{ fontSize: 10 }}>{item.status}</Tag>}
                </div>
              ),
              item,
            };
          })
        );
      } catch {
        setOptions([]);
      }
    }, 300);
  }, []);

  const handleSelect = useCallback((val) => {
    const selected = options.find((o) => o.value === val);
    if (selected && onNavigate) {
      onNavigate(selected.item);
    }
    setValue("");
    setOptions([]);
  }, [options, onNavigate]);

  return (
    <AutoComplete
      value={value}
      options={options}
      onSearch={handleSearch}
      onSelect={handleSelect}
      style={{ width: 260 }}
      popupMatchSelectWidth={400}
    >
      <Input
        prefix={<SearchOutlined />}
        placeholder="全局搜索..."
        allowClear
        onClear={() => setOptions([])}
        style={{ borderRadius: 20 }}
      />
    </AutoComplete>
  );
}
