#!/usr/bin/env python3
"""
ROI Calculator for HotPlex Issues
Scores issues based on Impact, Urgency, and Effort
"""

import json
import sys

def calculate_roi(impact, urgency, effort):
    """Calculate ROI score (1-100 scale)"""
    return (impact * urgency * effort) / 100

def estimate_effort_from_title(title):
    """Estimate effort from issue title patterns"""
    title_lower = title.lower()
    
    # Trivial fixes
    if any(x in title_lower for x in ['typo', 'spelling', 'minor', 'trivial']):
        return 10
    # Easy fixes
    if any(x in title_lower for x in ['simple', 'add', 'update', 'config']):
        return 8
    # Medium complexity
    if any(x in title_lower for x in ['refactor', 'improve', 'optimize']):
        return 6
    # Complex
    if any(x in title_lower for x in ['architecture', 'design', 'rewrite']):
        return 4
    # Very complex
    if any(x in title_lower for x in ['reimplement', 'major', 'complete']):
        return 2
    
    return 5  # Default medium effort

def estimate_impact_from_labels(labels):
    """Estimate impact from priority labels"""
    label_names = [l.get('name', '') for l in labels]
    
    if 'P1' in label_names or 'security' in label_names:
        return 10
    if 'P2' in label_names:
        return 8
    if 'P3' in label_names:
        return 5
    if 'documentation' in label_names:
        return 4
    
    return 6  # Default medium impact

def main():
    if len(sys.argv) < 2:
        print("Usage: calc-roi.py <issues.json>")
        sys.exit(1)
    
    with open(sys.argv[1], 'r') as f:
        issues = json.load(f)
    
    results = []
    
    for issue in issues:
        number = issue['number']
        title = issue['title']
        labels = issue.get('labels', [])
        
        # Estimate scores
        impact = estimate_impact_from_labels(labels)
        urgency = 5  # Default urgency
        effort = estimate_effort_from_title(title)
        
        # Calculate ROI
        roi = calculate_roi(impact, urgency, effort)
        
        # Determine priority
        if roi >= 50 or any(l.get('name') == 'P1' for l in labels):
            priority = 'P1'
        elif roi >= 30 or any(l.get('name') == 'P2' for l in labels):
            priority = 'P2'
        else:
            priority = 'P3'
        
        results.append({
            'number': number,
            'title': title,
            'impact': impact,
            'urgency': urgency,
            'effort': effort,
            'roi': round(roi, 1),
            'priority': priority
        })
    
    # Sort by ROI descending
    results.sort(key=lambda x: x['roi'], reverse=True)
    
    # Print results
    print(f"{'#':<4} {'Priority':<8} {'ROI':<6} {'I':<2} {'U':<2} {'E':<2} {'Title'}")
    print("-" * 100)
    
    for r in results[:20]:  # Top 20
        print(f"#{r['number']:<3} {r['priority']:<8} {r['roi']:<6.1f} {r['impact']:<2} {r['urgency']:<2} {r['effort']:<2} {r['title'][:60]}")

if __name__ == '__main__':
    main()
