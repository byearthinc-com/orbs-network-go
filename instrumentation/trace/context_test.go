// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

package trace

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestEntryPoint_DecoratesContext(t *testing.T) {
	ctx := NewContext(context.Background(), "foo")

	ep, ok := FromContext(ctx)

	require.True(t, ok)
	require.Equal(t, "foo", ep.name)
	require.NotEmpty(t, ep.requestId)
}

func TestNestedContextsRetainValue(t *testing.T) {
	ctx := NewContext(context.Background(), "foo")
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ep, ok := FromContext(childCtx)

	require.True(t, ok)
	require.Equal(t, "foo", ep.name)
	require.NotEmpty(t, ep.requestId)
}

func TestPropagateContextRetainsValue(t *testing.T) {
	ctx := NewContext(context.Background(), "foo")
	ep, ok := FromContext(ctx)

	anotherCtx := context.Background()
	propgatedTracingContext, ok := FromContext(PropagateContext(anotherCtx, ep))

	require.True(t, ok)
	require.Equal(t, "foo", propgatedTracingContext.name)
	require.NotEmpty(t, propgatedTracingContext.requestId)
}

func TestTranslateToRequestAndBack(t *testing.T) {
	ctx := NewContext(context.Background(), "foo")
	ep, _ := FromContext(ctx)

	request, _ := http.NewRequest("Get", "localhost", nil)
	ep.ToRequest(request)

	require.Equal(t, "foo", request.Header.Get(RequestTraceName))

	fctx := NewFromRequest(context.Background(), request)
	ep2, ok := FromContext(fctx)
	require.True(t, ok)
	require.Equal(t, ep.name, ep2.name)
	require.Equal(t, ep.requestId, ep2.requestId)
	// had to compare this "flat" as the now function of time adds a debug string for monotonic that i can't seem to clear otherwise
	require.EqualValues(t, ep.created.UnixNano(), ep2.created.UnixNano(), "%s %s", ep.created, ep2.created)
}
