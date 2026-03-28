import { h } from 'preact';
import { useState } from 'preact/hooks';
import { TabBar } from '../components/TabBar';
import { Memory } from './Memory';
import Graph from './Graph';

const TABS = [
  { id: 'memory', label: 'Memory' },
  { id: 'graph', label: 'Graph' },
];

export default function Knowledge() {
  // Read initial tab from URL query param.
  const params = new URLSearchParams(window.location.search);
  const [tab, setTab] = useState(params.get('tab') || 'memory');

  const changeTab = (id) => {
    setTab(id);
    const url = id === 'memory' ? '/knowledge' : `/knowledge?tab=${id}`;
    history.replaceState(null, '', url);
  };

  return (
    <div>
      <h1>Knowledge</h1>
      <TabBar tabs={TABS} active={tab} onChange={changeTab} />
      <div class="tab-content-enter" key={tab}>
        {tab === 'memory' && <Memory embedded />}
        {tab === 'graph' && <Graph />}
      </div>
    </div>
  );
}
