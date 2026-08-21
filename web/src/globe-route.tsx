import { useEffect, useMemo, useRef, useState } from 'react'
import Globe from 'react-globe.gl'
import { useMapData, type MapProps } from './map-data'
import type { RouteHop } from './types'

export default function GlobeRoute({ hops, selectedHop, onSelect }: MapProps) {
  const data = useMapData()
  const host = useRef<HTMLDivElement>(null)
  const globe = useRef<any>(null)
  const [width, setWidth] = useState(800)
  const known = useMemo(() => hops.filter((hop) => hop.latitude != null && hop.longitude != null), [hops])
  const arcs = useMemo(() => known.slice(1).map((hop, index) => ({
    startLat: known[index]!.latitude, startLng: known[index]!.longitude,
    endLat: hop.latitude, endLng: hop.longitude,
    unknown: hop.hop - known[index]!.hop > 1,
  })), [known])
  useEffect(() => {
    if (!host.current) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(300, entry?.contentRect.width ?? 800)))
    observer.observe(host.current)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    const hop = known.find((item) => item.hop === selectedHop)
    if (hop && globe.current) globe.current.pointOfView({ lat: hop.latitude, lng: hop.longitude, altitude: 1.7 }, matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 650)
  }, [selectedHop, known])
  if (!data) return <div className="map-loading" aria-busy="true" />
  return <div ref={host} className="route-map route-globe">
    <Globe ref={globe} width={width} height={Math.min(520, width * .62)} backgroundColor="rgba(0,0,0,0)"
      polygonsData={data.features} polygonAltitude={.006} polygonCapColor={() => 'rgba(90,125,155,.32)'} polygonSideColor={() => 'rgba(0,0,0,.08)'} polygonStrokeColor={() => 'rgba(160,190,210,.3)'}
      pointsData={known} pointLat="latitude" pointLng="longitude" pointAltitude={(point: object) => selectedHop === (point as RouteHop).hop ? .05 : .025} pointRadius={(point: object) => selectedHop === (point as RouteHop).hop ? .13 : .08} pointColor={(point: object) => (point as RouteHop).geoConfidence === 'approximate' ? '#f2b84b' : '#57c4ff'} onPointClick={(point: object) => onSelect((point as RouteHop).hop)}
      arcsData={arcs} arcStartLat="startLat" arcStartLng="startLng" arcEndLat="endLat" arcEndLng="endLng" arcColor={(arc: object) => (arc as { unknown: boolean }).unknown ? '#f2b84b' : '#57c4ff'} arcDashLength={(arc: object) => (arc as { unknown: boolean }).unknown ? .18 : 1} arcDashGap={(arc: object) => (arc as { unknown: boolean }).unknown ? .12 : 0} arcDashAnimateTime={matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 1600}
    />
  </div>
}
