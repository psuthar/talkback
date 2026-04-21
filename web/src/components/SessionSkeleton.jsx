import styles from './SessionSkeleton.module.css'

function SkeletonBar({ width = '100%', height = 14 }) {
  return <div className={styles.bar} style={{ width, height }} />
}

export function SessionSkeleton() {
  return (
    <div className={styles.root} data-testid="session-skeleton">
      <div className={styles.topbar}>
        <SkeletonBar width="220px" height={20} />
        <SkeletonBar width="70px" height={20} />
      </div>
      <div className={styles.grid}>
        <div className={styles.panel}>
          <SkeletonBar width="80%" />
          <SkeletonBar />
          <SkeletonBar width="60%" />
          <SkeletonBar width="90%" />
          <SkeletonBar width="70%" />
        </div>
        <div className={styles.center}>
          <div className={styles.videoBlock} />
          <SkeletonBar height={12} />
          <SkeletonBar width="75%" height={12} />
        </div>
        <div className={styles.panel}>
          <SkeletonBar width="90%" />
          <SkeletonBar width="60%" />
          <SkeletonBar />
          <SkeletonBar width="80%" />
        </div>
      </div>
    </div>
  )
}
