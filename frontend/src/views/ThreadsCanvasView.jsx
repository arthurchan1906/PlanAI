import { useMemo, useCallback, useEffect } from "react";
import {
  ReactFlow, MiniMap, Controls as RFControls, Background,
  useNodesState, useEdgesState, MarkerType, Handle, Position,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { forceSimulation, forceManyBody, forceLink, forceCenter, forceCollide } from "d3-force";
import { Empty, Spin, Typography, Tag, Popover, List } from "antd";
import {
  CheckCircleOutlined, SyncOutlined, ClockCircleOutlined,
  MinusCircleOutlined, BranchesOutlined,
} from "@ant-design/icons";

const { Text } = Typography;

const THREAD_COLORS = [
  "#2f6fec","#52c41a","#faad14","#eb2f96","#722ed1",
  "#13c2c2","#ff4d4f","#fa8c16","#a0d911","#2f54eb",
];
const PLAN_COLORS = [
  "#2f6fec","#52c41a","#faad14","#eb2f96","#722ed1",
  "#13c2c2","#ff4d4f","#fa8c16","#a0d911","#2f54eb",
];

function parseDate(s) { if(!s) return 0; const d=new Date(s); return isNaN(d.getTime())?0:d.getTime(); }
function fmtShort(s) { if(!s) return ""; const d=new Date(s); if(isNaN(d.getTime())) return s.slice(0,10); return `${d.getMonth()+1}/${d.getDate()}`; }

const planColorMap = new Map(); let pcIdx = 0;
function pColor(pid) { if(!planColorMap.has(pid)) planColorMap.set(pid,PLAN_COLORS[pcIdx++%PLAN_COLORS.length]); return planColorMap.get(pid); }

// ═══════════════════════════════════
// Force Layout Engine
// ═══════════════════════════════════
function forceLayout(taskNodes, linkPairs) {
  const nodeIds = new Set(taskNodes.map(n => n.id));
  const simNodes = taskNodes.map(n => ({ id: n.id }));
  const simLinks = linkPairs
    .filter(([s,t]) => nodeIds.has(s) && nodeIds.has(t))
    .map(([s,t]) => ({ source: s, target: t }));

  if (simLinks.length === 0) {
    // No edges — just spread nodes evenly
    const pos = {};
    const cols = Math.ceil(Math.sqrt(simNodes.length));
    simNodes.forEach((n, i) => {
      pos[n.id] = { x: (i % cols) * 180 - ((cols - 1) * 180) / 2, y: Math.floor(i / cols) * 70 };
    });
    return pos;
  }

  const sim = forceSimulation(simNodes)
    .force("charge", forceManyBody().strength(-400))
    .force("link", forceLink(simLinks).distance(140).strength(0.5))
    .force("center", forceCenter(0, 0))
    .force("collide", forceCollide(55))
    .stop();

  // Run simulation synchronously
  const N = Math.ceil(Math.log(sim.alphaMin()) / Math.log(1 - sim.alphaDecay()));
  for (let i = 0; i < N; i++) sim.tick();

  const pos = {};
  for (const n of sim.nodes()) pos[n.id] = { x: n.x, y: n.y };
  return pos;
}

// ═══════════════════════════════════
// Flat Task Node
// ═══════════════════════════════════
function TaskNode({ data }) {
  const s = data.status;
  const icon = s==="done"||s==="completed"
    ? <CheckCircleOutlined style={{color:"#52c41a",fontSize:11}} />
    : s==="in_progress"
    ? <SyncOutlined spin style={{color:"#2f6fec",fontSize:11}} />
    : s==="blocked"
    ? <MinusCircleOutlined style={{color:"#ff4d4f",fontSize:11}} />
    : <ClockCircleOutlined style={{color:"#8c8c8c",fontSize:11}} />;

  const commits = data.commits || [];

  return (
    <div className="rf-flat-node" style={{borderLeftColor:data.planColor||"#ddd"}}>
      <Handle type="source" position={Position.Bottom} id="s" style={{opacity:0}} />
      <Handle type="target" position={Position.Top} id="t" style={{opacity:0}} />
      <Handle type="source" position={Position.Right} id="r" style={{opacity:0}} />
      <Handle type="target" position={Position.Left} id="l" style={{opacity:0}} />

      <div className="rf-flat-row1">
        {icon}
        <Text style={{fontSize:10,lineHeight:"14px",flex:1,minWidth:0}} ellipsis={{tooltip:data.label}}>{data.label}</Text>
        {commits.length>0 && (
          <Popover trigger="hover" placement="right"
            content={<div style={{maxWidth:240,maxHeight:160,overflow:"auto"}}>
              <List size="small" dataSource={commits.slice(0,8)} renderItem={c=>(
                <List.Item style={{padding:"1px 0",border:"none"}}><Text style={{fontSize:10}} ellipsis={{tooltip:c.title}}>{c.title}</Text></List.Item>
              )} />
              {commits.length>8 && <Text type="secondary" style={{fontSize:10}}>+{commits.length-8}</Text>}
            </div>}
          ><span className="rf-flat-commit"><BranchesOutlined style={{fontSize:9}}/>{commits.length}</span></Popover>
        )}
      </div>
      <div className="rf-flat-row2">
        {data.planLabel && <Tag color={data.planColor} style={{fontSize:8,padding:"0 3px",lineHeight:"14px",margin:0,borderRadius:3}}>{data.planLabel}</Tag>}
        <Text type="secondary" style={{fontSize:9}}>{fmtShort(data.date)}</Text>
      </div>
    </div>
  );
}

function ThreadLabelNode({ data }) {
  return (
    <div style={{display:"flex",alignItems:"center",gap:4,padding:"1px 6px",whiteSpace:"nowrap"}}>
      <span style={{width:7,height:7,borderRadius:"50%",background:data.color,flexShrink:0}} />
      <Text style={{fontSize:10,color:"#444"}}>{data.label}</Text>
      {data.paused && <Tag color="warning" style={{fontSize:8,padding:"0 2px",lineHeight:"13px",margin:0}}>停</Tag>}
    </div>
  );
}

const nodeTypes = { taskItem: TaskNode, threadLabel: ThreadLabelNode };

// ═══════════════════════════════════
// Build Graph
// ═══════════════════════════════════
function buildGraph(threads, plans, tasks, commits, threadStatus) {
  const nodes = []; const edges = []; let nid=0; const nn=()=>`g${nid++}`;

  const taskCommits = {};
  for (const c of commits) { const tid=c.task_id; if(tid) (taskCommits[tid]=taskCommits[tid]||[]).push(c); }

  const planById = {}; for (const p of plans) planById[p.id]=p;

  // Show tasks: in threads OR in active plans
  const activePlanIds = new Set(plans.filter(p=>p.status==="active").map(p=>p.id));
  if (activePlanIds.size===0 && plans.length>0) plans.slice(0,3).forEach(p=>activePlanIds.add(p.id));
  const threadTasks = new Set();
  for (const th of threads) for (const it of th.items||[]) if (it.entity_type==="task") threadTasks.add(it.entity_id);

  const shown = tasks.filter(t=>threadTasks.has(t.id)||activePlanIds.has(t.plan_id)).slice(0,60);
  if (!shown.length) return {nodes,edges};

  // ── Task nodes ──
  const taskNodeIds = new Map(); // taskId → rfNodeId
  const taskNodes = [];          // for force layout
  const TW=178, TH=46;

  for (const t of shown) {
    const dn = nn();
    taskNodeIds.set(t.id, dn);
    taskNodes.push({ id: dn });
  }

  // ── Edges: thread edges + intra-plan edges ──
  const linkPairs = [];
  const threadEdgeSet = new Set(); // "src|tgt"

  // Thread edges
  const threadNodeMap = {}; // ti → [{nodeId, time}]
  for (let ti=0; ti<threads.length; ti++) {
    const th = threads[ti];
    const items = (th.items||[]).filter(i=>i.entity_type==="task"&&taskNodeIds.has(i.entity_id));
    if (items.length<2) continue;
    items.sort((a,b)=>{
      const ta=shown.find(t=>t.id===a.entity_id), tb=shown.find(t=>t.id===b.entity_id);
      return (parseDate(ta?.updated_at||ta?.created_at)||0)-(parseDate(tb?.updated_at||tb?.created_at)||0);
    });
    (threadNodeMap[ti]=threadNodeMap[ti]||[]).push(...items.map(i=>taskNodeIds.get(i.entity_id)));
    for (let i=1; i<items.length; i++) {
      const s=taskNodeIds.get(items[i-1].entity_id), t=taskNodeIds.get(items[i].entity_id);
      if (s&&t) { linkPairs.push([s,t]); threadEdgeSet.add(`${s}|${t}`); }
    }
  }

  // Intra-plan edges (for graph structure)
  const planGroups = {};
  for (const t of shown) { const pid=t.plan_id||"__x__"; (planGroups[pid]=planGroups[pid]||[]).push(t); }
  for (const pts of Object.values(planGroups)) {
    if (pts.length<2) continue;
    const chain = pts.map(t=>taskNodeIds.get(t.id)).filter(Boolean);
    for (let i=1; i<chain.length; i++) {
      const key = `${chain[i-1]}|${chain[i]}`;
      if (!threadEdgeSet.has(key)) { linkPairs.push([chain[i-1],chain[i]]); }
    }
  }

  // ── Force layout ──
  const positions = forceLayout(taskNodes, linkPairs);

  // ── Build react-flow nodes ──
  for (const t of shown) {
    const dn = taskNodeIds.get(t.id); if (!dn) continue;
    const pos = positions[dn]; if (!pos) continue;
    const pid = t.plan_id||"";
    const plan = pid ? planById[pid] : null;
    const mc = taskCommits[t.id]||[];
    const pc = pColor(pid||"__x__");
    nodes.push({
      id: dn, type: "taskItem",
      position: { x: pos.x - TW/2, y: pos.y - TH/2 },
      data: {
        label: t.title||t.id, status: t.status||"todo",
        date: t.updated_at||t.created_at,
        planLabel: plan ? (plan.title||plan.id).slice(0,12) : "",
        planColor: pc,
        commitCount: mc.length,
        commits: mc.map(c=>({id:c.id,title:c.title})),
      },
      style: { width: TW, height: TH },
    });
  }

  // ── Edges ──
  // Thread edges (colored, animated)
  for (let ti=0; ti<threads.length; ti++) {
    const color = THREAD_COLORS[ti%THREAD_COLORS.length];
    const ids = threadNodeMap[ti]||[];
    for (let i=1; i<ids.length; i++) {
      edges.push({
        id: `th-${ti}-${i}`,
        source: ids[i-1], target: ids[i],
        type: "smoothstep", animated: true,
        style: { stroke: color, strokeWidth: 1.8, strokeDasharray:"5 3", opacity:0.55 },
        markerEnd: { type: MarkerType.ArrowClosed, color, width:6, height:6 },
        interactionWidth: 6,
      });
    }
  }
  // Intra-plan edges (thin, gray)
  for (const pts of Object.values(planGroups)) {
    const chain = pts.map(t=>taskNodeIds.get(t.id)).filter(Boolean);
    for (let i=1; i<chain.length; i++) {
      const key = `${chain[i-1]}|${chain[i]}`;
      if (!threadEdgeSet.has(key)) {
        edges.push({
          id: `pe-${chain[i-1]}-${chain[i]}`,
          source: chain[i-1], target: chain[i],
          type: "smoothstep",
          style: { stroke: "#ddd", strokeWidth: 0.6, strokeDasharray:"3 5", opacity:0.3 },
          interactionWidth: 3,
        });
      }
    }
  }

  // ── Thread labels at top ──
  const allY = nodes.map(n=>n.position.y), allX = nodes.map(n=>n.position.x);
  const topY = Math.min(...allY)-34, leftX = Math.min(...allX);
  for (let ti=0; ti<threads.length; ti++) {
    const th = threads[ti];
    const nds = threadNodeMap[ti];
    if (!nds||nds.length<2) continue;
    const color = THREAD_COLORS[ti%THREAD_COLORS.length];
    const paused = threadStatus?.find(s=>s.thread_id===th.id)?.paused;
    nodes.push({
      id: nn(), type: "threadLabel",
      position: { x: leftX+ti*140, y: topY },
      data: { label: th.title, color, paused },
      style: { width:"auto" },
      selectable: false, draggable: true,
    });
  }

  return {nodes, edges};
}

// ═══════════════════════════════════
export default function ThreadsCanvasView({ threads, plans, tasks, commits, decisions, threadSuggestions, threadStatus, loading }) {
  const { nodes: initNodes, edges: initEdges } = useMemo(
    () => buildGraph(threads, plans, tasks, commits, threadStatus),
    [threads, plans, tasks, commits, threadStatus]
  );
  const [nodes, setNodes, onNodesChange] = useNodesState(initNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initEdges);

  useEffect(() => { setNodes(initNodes); setEdges(initEdges); }, [initNodes, initEdges]);

  const onInit = useCallback(inst => { setTimeout(()=>inst.fitView({padding:0.1,duration:400}),100); },[]);

  if (loading) return <div style={{textAlign:"center",padding:60}}><Spin size="large"/></div>;
  if (!threads.length && !plans.length) return <Empty description="暂无数据"/>;

  return (
    <div style={{height:"calc(100vh - 140px)",width:"100%"}}>
      <ReactFlow
        nodes={nodes} edges={edges}
        onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes} onInit={onInit}
        fitView minZoom={0.08} maxZoom={2}
        nodesDraggable={false} nodesConnectable={false}
        defaultEdgeOptions={{type:"smoothstep",style:{stroke:"#ddd",strokeWidth:1}}}
        proOptions={{hideAttribution:true}}
      >
        <MiniMap nodeStrokeWidth={2} pannable style={{width:130,height:80}} maskColor="rgba(0,0,0,0.04)"/>
        <RFControls showInteractive={false}/>
        <Background gap={20} size={0.5} color="#e8e8e8"/>
      </ReactFlow>
    </div>
  );
}
