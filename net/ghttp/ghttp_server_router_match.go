package ghttp

import (
	"strings"

	"github.com/gogf/gf/v2/encoding/gurl"
	"github.com/gogf/gf/v2/text/gregex"
)

type routeMatcherMode uint8

const (
	routeMatcherModeStatic routeMatcherMode = iota
	routeMatcherModeSegments
	routeMatcherModeRegex
)

type routeSegmentMatcherKind uint8

const (
	routeSegmentMatcherKindStatic routeSegmentMatcherKind = iota
	routeSegmentMatcherKindNamed
	routeSegmentMatcherKindCatchAll
	routeSegmentMatcherKindPattern
)

type routeMatcher struct {
	mode         routeMatcherMode
	path         string
	segments     []routeSegmentMatcher
	captureCount int
}

type routeSegmentMatcher struct {
	kind         routeSegmentMatcherKind
	value        string
	parts        []routeSegmentPart
	captureCount int
}

type routeSegmentPart struct {
	static string
	name   string
}

type routePatternCapture struct {
	name  string
	value string
}

func (r *Router) match(path string, parts []string) (matched bool, values map[string]string) {
	if r.matcher == nil || r.matcher.mode == routeMatcherModeRegex {
		return r.matchWithRegex(path)
	}
	return r.matcher.match(path, parts)
}

func (r *Router) matchWithRegex(path string) (matched bool, values map[string]string) {
	match, err := gregex.MatchString(r.RegRule, path)
	if err != nil || len(match) == 0 {
		return false, nil
	}
	if len(r.RegNames) > 0 && len(match) > len(r.RegNames) {
		values = make(map[string]string, len(r.RegNames))
		for i, name := range r.RegNames {
			values[name], _ = gurl.Decode(match[i+1])
		}
	}
	return true, values
}

func (m *routeMatcher) match(path string, parts []string) (matched bool, values map[string]string) {
	switch m.mode {
	case routeMatcherModeStatic:
		return path == m.path, nil

	case routeMatcherModeSegments:
		if len(m.segments) == 0 {
			return len(parts) == 1 && parts[0] == "/", nil
		}
		partIndex := 0
		for segmentIndex, segment := range m.segments {
			switch segment.kind {
			case routeSegmentMatcherKindCatchAll:
				if segment.value != "" {
					captured := captureRouterCatchAllValue(path, partIndex, len(parts))
					if captured != "" {
						if values == nil {
							values = m.makeValues()
						}
						values[segment.value], _ = gurl.Decode(captured)
					} else if values == nil {
						values = m.makeValues()
						values[segment.value] = ""
					}
				}
				return true, values

			default:
				if partIndex >= len(parts) {
					return false, nil
				}
				part := parts[partIndex]
				partIndex++

				switch segment.kind {
				case routeSegmentMatcherKindStatic:
					if part != segment.value {
						return false, nil
					}

				case routeSegmentMatcherKindNamed:
					if part == "" {
						return false, nil
					}
					if segment.value != "" {
						if values == nil {
							values = m.makeValues()
						}
						values[segment.value], _ = gurl.Decode(part)
					}

				case routeSegmentMatcherKindPattern:
					matched, values = matchRoutePatternSegment(segment.parts, part, values, m.captureCount)
					if !matched {
						return false, nil
					}
				}
			}

			if segmentIndex == len(m.segments)-1 && partIndex != len(parts) {
				return false, nil
			}
		}
		return partIndex == len(parts), values
	}
	return false, nil
}

func (m *routeMatcher) makeValues() map[string]string {
	capacity := m.captureCount
	if capacity < 1 {
		capacity = 1
	}
	// Route parameter count is known at registration time, so allocate enough
	// buckets up front and avoid map growth on multi-parameter routes.
	return make(map[string]string, capacity)
}

func captureRouterCatchAllValue(path string, partIndex, partCount int) string {
	// Slice from the original path instead of joining remaining segments.
	if partIndex >= partCount {
		return ""
	}
	if partIndex == 0 {
		if path == "/" {
			return ""
		}
		return path[1:]
	}
	for i, seenParts := 1, 0; i < len(path); i++ {
		if path[i] != '/' {
			continue
		}
		seenParts++
		if seenParts == partIndex {
			return path[i+1:]
		}
	}
	return ""
}

func newRouteMatcher(rule string) *routeMatcher {
	if rule == "/" {
		return &routeMatcher{mode: routeMatcherModeStatic, path: rule}
	}
	segments := strings.Split(rule[1:], "/")
	matcher := &routeMatcher{
		mode:     routeMatcherModeSegments,
		segments: make([]routeSegmentMatcher, 0, len(segments)),
	}
	hasDynamicSegment := false
	for index, segment := range segments {
		if segment == "" {
			continue
		}
		compiledSegment, ok := compileRouteSegmentMatcher(segment)
		if !ok {
			return &routeMatcher{mode: routeMatcherModeRegex}
		}
		matcher.segments = append(matcher.segments, compiledSegment)
		matcher.captureCount += compiledSegment.captureCount
		if compiledSegment.kind != routeSegmentMatcherKindStatic {
			hasDynamicSegment = true
		}
		if compiledSegment.kind == routeSegmentMatcherKindCatchAll && index != len(segments)-1 {
			// Catch-all segments can only appear at the tail for the fast path matcher.
			return &routeMatcher{mode: routeMatcherModeRegex}
		}
	}
	if !hasDynamicSegment {
		// Static routes have already reached their leaf node in the route tree; the full path check is enough.
		return &routeMatcher{mode: routeMatcherModeStatic, path: rule}
	}
	return matcher
}

func compileRouteSegmentMatcher(segment string) (routeSegmentMatcher, bool) {
	switch segment[0] {
	case ':':
		return routeSegmentMatcher{
			kind:         routeSegmentMatcherKindNamed,
			value:        segment[1:],
			captureCount: 1,
		}, true

	case '*':
		return routeSegmentMatcher{
			kind:         routeSegmentMatcherKindCatchAll,
			value:        segment[1:],
			captureCount: 1,
		}, true
	}

	if strings.Contains(segment, "*") {
		return routeSegmentMatcher{}, false
	}
	if !strings.Contains(segment, "{") {
		return routeSegmentMatcher{
			kind:  routeSegmentMatcherKindStatic,
			value: segment,
		}, true
	}

	parts, captureCount, ok := compileRoutePatternParts(segment)
	if !ok {
		return routeSegmentMatcher{}, false
	}
	return routeSegmentMatcher{
		kind:         routeSegmentMatcherKindPattern,
		parts:        parts,
		captureCount: captureCount,
	}, true
}

func compileRoutePatternParts(segment string) ([]routeSegmentPart, int, bool) {
	parts := make([]routeSegmentPart, 0, 4)
	captureCount := 0
	for index := 0; index < len(segment); {
		if segment[index] != '{' {
			nextIndex := strings.IndexByte(segment[index:], '{')
			if nextIndex < 0 {
				parts = append(parts, routeSegmentPart{static: segment[index:]})
				break
			}
			parts = append(parts, routeSegmentPart{static: segment[index : index+nextIndex]})
			index += nextIndex
			continue
		}

		endIndex := strings.IndexByte(segment[index+1:], '}')
		if endIndex < 0 {
			return nil, 0, false
		}
		name := segment[index+1 : index+1+endIndex]
		if name == "" {
			return nil, 0, false
		}
		if len(parts) > 0 && parts[len(parts)-1].name != "" {
			return nil, 0, false
		}
		parts = append(parts, routeSegmentPart{name: name})
		captureCount++
		index += endIndex + 2
	}
	return parts, captureCount, len(parts) > 0
}

func matchRoutePatternSegment(
	parts []routeSegmentPart, segment string, values map[string]string, valueCapacity int,
) (bool, map[string]string) {
	// Pattern captures are usually tiny, so keep them on stack instead of allocating a temporary map.
	var captureBuffer [4]routePatternCapture
	captures := captureBuffer[:0]
	matched, captures := matchRoutePatternParts(parts, 0, segment, 0, captures)
	if !matched {
		return false, nil
	}
	if len(captures) == 0 {
		return true, values
	}
	if values == nil {
		if valueCapacity < len(captures) {
			valueCapacity = len(captures)
		}
		values = make(map[string]string, valueCapacity)
	}
	for _, capture := range captures {
		values[capture.name], _ = gurl.Decode(capture.value)
	}
	return true, values
}

func matchRoutePatternParts(
	parts []routeSegmentPart, partIndex int, segment string, position int, captures []routePatternCapture,
) (bool, []routePatternCapture) {
	if partIndex >= len(parts) {
		return position == len(segment), captures
	}
	part := parts[partIndex]
	if part.name == "" {
		if !strings.HasPrefix(segment[position:], part.static) {
			return false, captures
		}
		return matchRoutePatternParts(parts, partIndex+1, segment, position+len(part.static), captures)
	}
	if position >= len(segment) {
		return false, captures
	}

	nextStatic := ""
	if partIndex+1 < len(parts) && parts[partIndex+1].name == "" {
		nextStatic = parts[partIndex+1].static
	}
	if nextStatic == "" {
		captures = append(captures, routePatternCapture{
			name:  part.name,
			value: segment[position:],
		})
		return matchRoutePatternParts(parts, partIndex+1, segment, len(segment), captures)
	}

	searchValue := segment[position:]
	searchEnd := len(searchValue)
	for searchEnd >= 0 {
		foundIndex := strings.LastIndex(searchValue[:searchEnd], nextStatic)
		if foundIndex < 0 {
			break
		}
		if foundIndex > 0 {
			captureCount := len(captures)
			captures = append(captures, routePatternCapture{
				name:  part.name,
				value: searchValue[:foundIndex],
			})
			if matched, matchedCaptures := matchRoutePatternParts(
				parts, partIndex+1, segment, position+foundIndex, captures,
			); matched {
				return true, matchedCaptures
			}
			captures = captures[:captureCount]
		}
		searchEnd = foundIndex
	}
	return false, captures
}
